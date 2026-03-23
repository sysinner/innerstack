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

package signals

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const maxSignals = 128

var (
	mu sync.Mutex

	regQueue = make(chan int, maxSignals)

	sigQueue = make(chan int, 1)

	done      = false
	doneQueue = make(chan int, maxSignals)

	shutdowns = []func(){}
)

func Go(start, shutdown func()) {

	mu.Lock()
	defer mu.Unlock()

	if start == nil || done {
		return
	}

	if len(regQueue) >= maxSignals {
		panic(fmt.Sprintf("too many signals (limits %d)", maxSignals))
	}
	regQueue <- 1

	if shutdown != nil {
		shutdowns = append(shutdowns, shutdown)
	}

	go func() {
		start()
		<-regQueue
		if done {
			<-sigQueue
		}
	}()
}

func Done() <-chan int {
	return doneQueue
}

func Quit() bool {
	return done
}

func Wait() {
	quit := make(chan os.Signal, 2)

	//
	signal.Notify(quit,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGKILL)
	sg := <-quit
	slog.Warn(fmt.Sprintf("Signal %s ...", sg.String()))

	mu.Lock()
	defer mu.Unlock()

	done = true

	for _, shutdown := range shutdowns {
		shutdown()
	}

	if n := len(regQueue); n > 0 {
		for i := 0; i < n; i++ {
			doneQueue <- 1
		}

		for i := 0; i < n; i++ {
			sigQueue <- 1
		}

		sigQueue <- 1
	}

	slog.Warn(fmt.Sprintf("Signal %s ... Done", sg.String()))
}
