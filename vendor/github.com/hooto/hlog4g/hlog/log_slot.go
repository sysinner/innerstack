// Copyright 2013 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

package hlog

import (
	"strings"
	"sync"
	"time"
)

var (
	slots  = map[string]*slotEntry{}
	slotmu sync.RWMutex
	slott  = int64(0)
)

type slotEntry struct {
	ttl  time.Time // time to log
	args []interface{}
}

func slotTime(tn time.Time, sec int64) time.Time {
	if sec < 1 {
		sec = 1
	} else if sec > 3600 {
		sec = 3600
	}
	fix := int64(tn.Second())
	if sec > 60 {
		fix += int64(tn.Minute() * 60)
	}
	if fix = fix % sec; fix > 0 {
		tn = tn.Add(time.Second * time.Duration(sec-fix))
	}
	return time.Unix(tn.Unix(), 0)
}

func SlotPrint(sec int64, levelTag, format string, a ...interface{}) {
	levelTag = strings.ToUpper(levelTag)
	level, ok := levelMap[levelTag]
	if !ok || level < minLogLevel {
		return
	}

	tn := time.Now()

	slotmu.Lock()
	defer slotmu.Unlock()

	if p, ok := slots[format]; !ok {
		p = &slotEntry{
			ttl:  slotTime(tn, sec),
			args: a,
		}
		slots[format] = p
	} else if tn.Unix() > p.ttl.Unix() {
		newEntry(p.ttl, printFormat, levelTag, format, p.args...)
		slots[format] = &slotEntry{
			ttl:  slotTime(tn, sec),
			args: a,
		}
	} else {
		p.args = a
	}

	const limit = 1000
	if (slott + 60) < tn.Unix() {
		rkeys := []string{}
		for k, v := range slots {
			if tn.Unix() <= v.ttl.Unix() {
				continue
			}
			rkeys = append(rkeys, k)
		}
		if len(rkeys) > 0 {
			newEntry(time.Now(), printFormat, "warn", "hlog slots auto clean %d/%d (#1)",
				len(rkeys), len(slots))
			for _, k := range rkeys {
				delete(slots, k)
			}
		}
		slott = tn.Unix()
	}
	if len(slots) >= limit {
		rkeys := []string{}
		for k, _ := range slots {
			rkeys = append(rkeys, k)
			if len(rkeys) >= limit/2 {
				break
			}
		}
		for _, k := range rkeys {
			delete(slots, k)
		}
		newEntry(time.Now(), printFormat, "warn", "hlog slots auto clean %d/%d (#2)",
			len(rkeys), len(slots))
	}
}
