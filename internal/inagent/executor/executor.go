// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package executor

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/inagent/status"
	"github.com/sysinner/incore/v2/internal/inutil/tplrender"
)

func oplogName(name string) string {
	return "box/exec/" + name
}

func keyenc(k string) string {
	return strings.Replace(strings.Replace(k, "/", "__", -1), "-", "_", -1)
}

func repParams(app *hostapi.AppReplicaInstance) map[string]string {

	sets := map[string]string{}

	sets["app__id"] = app.App.Id
	sets["app__replica__rep_id"] = fmt.Sprintf("%d", app.Replica.Id)
	sets["app__operate__replica_cap"] = fmt.Sprintf("%d", app.App.Operate.ReplicaCap)

	for _, opt := range app.App.Operate.Options {

		for _, item := range opt.Items {
			var (
				ckey = keyenc(fmt.Sprintf("%s__%s",
					opt.Name, item.Name))
				key = keyenc(fmt.Sprintf("app__%s__option__%s",
					app.App.Spec.Name, ckey))
			)
			sets[key] = item.Value
			if _, ok := sets[ckey]; !ok {
				sets[ckey] = item.Value
			}
		}
	}

	for _, p := range app.App.Operate.Services {

		if p.Name == "" || len(p.Endpoints) < 1 {
			continue
		}

		key := keyenc(fmt.Sprintf("app__oprep__port__%s__",
			p.Name,
		))
		sets[key+"lan_addr"] = p.Endpoints[0].Ip
		sets[key+"host_port"] = fmt.Sprintf("%d", p.Endpoints[0].Port)
	}

	for _, p := range app.App.Spec.Packages {
		sets[fmt.Sprintf("inpack_prefix_%s", strings.Replace(p.Name, "-", "_", -1))] =
			fmt.Sprintf("/usr/sysinner/%s/%s", p.Name, p.Version)
	}

	return sets
}

func execStatusName(id string, name string) string {
	return (fmt.Sprintf("%s/%s", id, name))
}

func execAction(
	app *hostapi.AppReplicaInstance,
	homeDir, specName, execName, action string,
) error {

	sets := repParams(app)

	for i := 1; i < 10; i++ {

		n := 0

		if specName != "" && specName != app.App.Spec.Name {
			continue
		}
		for _, ve := range app.App.Spec.Executors {
			if execName != "" && execName != ve.Name {
				continue
			}
			n += 1
			esName := execStatusName(app.App.Spec.Name, ve.Name)
			if sts, _ := executorAction(esName, ve, sets, inapi.OpActionStop); sts == inapi.OpLogOK {
				n -= 1
			}
		}

		if n == 0 {
			return nil
		}

		time.Sleep(time.Duration(i) * time.Second)
	}

	return errors.New("timeout in execAction")
}

func Restart(pod *hostapi.AppReplicaInstance, homeDir, specName, execName string) error {
	if err := execAction(pod, homeDir, specName, execName, inapi.OpActionStop); err != nil {
		return err
	}
	return execAction(pod, homeDir, specName, execName, inapi.OpActionStart)
}

func StopAll(pod *hostapi.AppReplicaInstance, homeDir string) error {
	return execAction(pod, homeDir, "", "", inapi.OpActionStop)
}

func Runner(app *hostapi.AppReplicaInstance, homeDir string) error {

	if len(app.App.Spec.Executors) == 0 {
		return nil
	}

	sets := repParams(app)

	for priority := uint32(0); priority <= uint32(inapi.SpecExecutorPriorityMax); {

		pdone := 0

		if app.App.Operate.Action != inapi.OpActionStart &&
			app.App.Operate.Action != inapi.OpActionStop {
			break
		}

		for _, ve := range app.App.Spec.Executors {

			if priority != ve.Priority {
				continue
			}

			pdone++

			esName := execStatusName(app.App.Spec.Name, ve.Name)

			if es := status.ExecStatusSet.Get(esName); es != nil {
				if es.Action.Allow(inapi.ExecutorActionStarted) ||
					es.Action.Allow(inapi.ExecutorActionStopped) {
					pdone--
				}
			}

			status.Executors.Sync(ve)
			if sts, msg := executorAction(esName, ve, sets, app.App.Operate.Action); sts != "" {
				// status.OpLog.LogSet(pod.App.Operate.Version, oplogName(string(esName)), sts, msg)
				slog.Info(fmt.Sprintf("exec stats %s, msg %s", sts, msg))
			}
		}

		if pdone == 0 {
			priority++
		} else {
			time.Sleep(1e9)
		}
	}

	return nil
}

func executorAction(
	esName string, etr *inapi.AppExecutor,
	dms map[string]string, opAction string,
) (string, string) {

	if etr.Plan == nil {
		etr.Plan = &inapi.AppSpecExecPlanner{
			OnBoot: true,
		}
	}

	es := status.ExecStatusSet.Get(esName)
	opStatus, opMsg := "", ""

	//
	if es == nil {

		es = &inapi.ExecutorStatus{
			Name:    esName,
			Created: time.Now().Unix(),
			Vendor:  etr.Vendor,
		}

		status.ExecStatusSet.Sync(es)
	}

	//
	if es.Cmd != nil {

		if es.Cmd.Process != nil {

			if es.Cmd.ProcessState == nil {
				es.Action.Append(inapi.ExecutorActionPending)
			}
		}

		if es.Cmd.ProcessState != nil && es.Cmd.ProcessState.Exited() {

			es.Action.Remove(inapi.ExecutorActionPending)

			if es.Cmd.ProcessState.Success() {
				es.Action.Remove(inapi.ExecutorActionFailed)
				opStatus, opMsg = inapi.OpLogOK, "process ok"
			} else {
				es.Action.Append(inapi.ExecutorActionFailed)
				opStatus = inapi.OpLogError
				// capture output from buffer
				es.Output = strings.TrimSpace(string(es.OutputBuf))
				if es.Output != "" {
					opMsg = fmt.Sprintf("process error %s, output: %s",
						es.Cmd.ProcessState.String(), es.Output)
				} else {
					opMsg = "process error " + es.Cmd.ProcessState.String()
				}
			}

			if es.Action.Allow(inapi.ExecutorActionStart) {
				es.Action.Remove(inapi.ExecutorActionStart)
				es.Action.Append(inapi.ExecutorActionStarted)
			}

			if es.Action.Allow(inapi.ExecutorActionStop) {
				es.Action.Remove(inapi.ExecutorActionStop)
				es.Action.Append(inapi.ExecutorActionStopped)
			}

			slog.Info("executor done", "name", esName, "status", es.Action.String())

			if es.Cmd.Process != nil {
				es.Cmd.Process.Kill()
				time.Sleep(5e8)
			}

			es.Cmd = nil
			es.Updated = time.Now().Unix()

			return opStatus, opMsg
		}

	}

	if opAction == inapi.OpActionStop &&
		es.Action.Allow(inapi.ExecutorActionStopped) {
		return inapi.OpLogOK, "stopped"
	}

	//
	// slog.Debug("executor action", "name", etr.Name, "action", es.Action.String())
	if es.Action.Allow(inapi.ExecutorActionPending) {
		slog.Debug("executor Cmd.ProcessState Pending SKIP", "name", esName)
		return inapi.OpLogInfo, "pending"
	}

	// Exec Planner
	if opAction == inapi.OpActionStop {
		es.Action = inapi.ExecutorActionStop
	} else if opAction == inapi.OpActionStart && etr.Plan != nil {

		es.Action.Remove(inapi.ExecutorActionStop)
		es.Action.Remove(inapi.ExecutorActionStopped)

		//
		if etr.Plan != nil &&
			etr.Plan.OnBoot &&
			es.Plan.Updated < 1 &&
			!es.Action.Allow(inapi.ExecutorActionStarted) {

			es.Action.Append(inapi.ExecutorActionStart)

			slog.Warn("executor Plan.OnBoot Exec", "name", esName)
		}

		//
		if etr.Plan.OnCalendar != "" &&
			!es.Action.Allow(inapi.ExecutorActionStart) {
			// TODO
		}

		//
		if etr.Plan.OnTick > 0 &&
			!es.Action.Allow(inapi.ExecutorActionStart) {

			if etr.Plan.OnTick < 60 {
				etr.Plan.OnTick = 60
			}

			if (time.Now().Unix() - es.Plan.Updated) > int64(etr.Plan.OnTick) {

				es.Action.Append(inapi.ExecutorActionStart)
				es.Action.Remove(inapi.ExecutorActionStarted)

				slog.Info("executor Plan.OnTick", "name", esName)
			}

		}

		//
		if etr.Plan.OnFailed != nil &&
			!es.Action.Allow(inapi.ExecutorActionStart) &&
			es.Action.Allow(inapi.ExecutorActionFailed) {

			retrySec := inapi.ExecPlanTimer(etr.Plan.OnFailed.RetrySec).Seconds()
			if retrySec < 1 {
				retrySec = 10
			}

			if es.Plan.Updated > 0 &&
				(etr.Plan.OnFailed.RetryMax == -1 ||
					es.Plan.OnFailedRetryNum < int(etr.Plan.OnFailed.RetryMax)) &&
				(time.Now().Unix()-es.Plan.Updated) > retrySec {

				es.Action.Append(inapi.ExecutorActionStart)
				es.Action.Remove(inapi.ExecutorActionStarted)

				es.Plan.OnFailedRetryNum++

				slog.Warn("executor Plan.OnFailed Retry",
					"name", esName, "retry_num", es.Plan.OnFailedRetryNum)
			}
		}

	} else {
		slog.Info("inagent/exec run ...")

		return "", ""
	}

	//
	script := ""
	if es.Action.Allow(inapi.ExecutorActionStart) {
		script = etr.StartScript
	} else if es.Action.Allow(inapi.ExecutorActionStop) {
		script = etr.StopScript
	} else {
		return "", ""
	}

	slog.Info("inagent/exec run ...")

	//
	es.Action.Append(inapi.ExecutorActionPending)
	es.Plan.Updated = time.Now().Unix()
	if es.Cmd == nil {
		es.Cmd = exec.Command("sh")
	}

	slog.Info("inagent/exec run ...")

	//
	bs, err := tplrender.Render(script, dms)
	if err != nil {
		slog.Error("executor template.Execute", "name", esName, "error", err)
		return inapi.OpLogError, err.Error()
	}
	vars := ""
	for k, v := range dms {
		if vars != "" {
			vars += ","
		}
		vars += fmt.Sprintf("%s=%s", k, v)
	}
	slog.Debug("executor exec", "name", esName, "vars", vars, "script", string(bs))

	//
	if err := executorCmd(es, string(bs)); err != nil {
		slog.Error("executor CMD", "name", esName, "error", err)
		return inapi.OpLogError, err.Error()
	}

	return inapi.OpLogInfo, "pending"
}

func executorCmd(es *inapi.ExecutorStatus, script string) error {

	if es == nil || es.Cmd == nil {
		return errors.New("No Command INIT")
	}

	cmd := es.Cmd

	if cmd.Process != nil && cmd.ProcessState == nil {
		return errors.New("Command Pending")
	}

	if cmd.ProcessState != nil {
		return nil
	}

	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	// setup stdout/stderr capture using OutputBuf
	es.OutputBuf = make([]byte, 0, 4096)
	outputWriter := &outputBuffer{buf: &es.OutputBuf}
	cmd.Stdout = outputWriter
	cmd.Stderr = outputWriter

	in.Write([]byte("set -e\n" + script + "\nexit\n"))
	in.Close()

	if err := cmd.Start(); err != nil {
		return err
	}

	go cmd.Wait()

	return nil
}

// outputBuffer is a thread-safe writer that appends to a byte slice
type outputBuffer struct {
	buf *[]byte
}

func (ob *outputBuffer) Write(p []byte) (n int, err error) {
	*ob.buf = append(*ob.buf, p...)
	return len(p), nil
}
