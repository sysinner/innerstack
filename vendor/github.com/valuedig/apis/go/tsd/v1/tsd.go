// Copyright 2020 Eryx <evorui аt gmail dοt com>, All rights reserved.
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

package tsd

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	CycleKeysMax                    = 100000
	CycleUnitMax              int64 = 3600
	CycleUnitMin              int64 = 1
	cycleExportOptionsTimeMax int64 = 31 * 86400
)

const (
	ValueAttrSum uint64 = 1 << 2
)

var (
	cycleFeedMU  sync.RWMutex
	cycleEntryMU sync.RWMutex
)

func NewCycleFeed(unit int64) *CycleFeed {
	if fix := unit % CycleUnitMin; fix > 0 {
		unit -= unit
	}
	if unit < CycleUnitMin {
		unit = CycleUnitMin
	} else if unit > CycleUnitMax {
		unit = CycleUnitMax
	}
	return &CycleFeed{
		Unit: unit,
	}
}

func (it *CycleFeed) Entry(name string) *CycleEntry {

	cycleFeedMU.Lock()
	defer cycleFeedMU.Unlock()

	for _, v := range it.Items {
		if name == v.Name {
			return v
		}
	}
	entry := &CycleEntry{
		Name: name,
		Unit: it.Unit,
	}
	it.Items = append(it.Items, entry)
	return entry
}

func (it *CycleFeed) Sync(name string, key, value int64, attrs uint64) {
	it.Entry(name).Sync(key, value, attrs)
}

func (it *CycleFeed) Trim(sec int64) {

	if sec < 1 {
		sec = 1
	}
	tn := time.Now().Unix()
	tl := tn - sec

	cycleFeedMU.Lock()
	defer cycleFeedMU.Unlock()

	for _, v := range it.Items {
		for i, v2 := range v.Keys {
			if v2 > tl {
				continue
			}
			v.Keys = v.Keys[i:]
			v.Values = v.Values[i:]
			break
		}
	}
}

func (it *CycleEntry) add(key, value int64) {
	for i := 0; i < len(it.Keys); i++ {

		if key > it.Keys[i] {
			continue
		}

		if key == it.Keys[i] {
			it.Values[i] += value
		} else {
			it.Keys = append(append(it.Keys[:i], key), it.Keys[i:]...)
			it.Values = append(append(it.Values[:i], value), it.Values[i:]...)
		}

		return
	}

	it.Keys = append(it.Keys, key)
	it.Values = append(it.Values, value)
}

func (it *CycleEntry) set(keys []int64, key, value int64, attrs uint64) {
	if len(keys) > 0 {

		for i := 0; i < len(keys)-1; i++ {
			if key >= keys[i] && key < keys[i+1] {
				if u64Allow(it.Attrs|attrs, ValueAttrSum) {
					it.Values[i] += value
				} else {
					it.Values[i] = value
				}
				return
			}
		}

		if key >= keys[len(keys)-1] {
			if u64Allow(it.Attrs|attrs, ValueAttrSum) {
				it.Values[len(keys)-1] += value
				if attrs > 0 {
					it.Attrs |= attrs
				}
			} else {
				it.Values[len(keys)-1] = value
			}
		}
	}
}

func (it *CycleEntry) Sync(key, value int64, attrs uint64) {

	var kt time.Time

	if key <= 0 {
		kt = time.Now()
		key = kt.Unix()
	} else {
		kt = time.Unix(key, 0)
	}

	if it.Unit < 1 {
		it.Unit = 1
	}

	if it.Unit >= 3600 {
		key -= int64(kt.Minute()) * 60
		key -= int64(kt.Second())
	} else if it.Unit >= 60 {
		key -= (int64(kt.Minute()*60) % it.Unit)
		key -= int64(kt.Second())
	} else {
		key -= int64(kt.Second()) % it.Unit
	}

	cycleEntryMU.Lock()
	defer cycleEntryMU.Unlock()

	if len(it.Keys) != len(it.Values) {
		it.Keys, it.Values = nil, nil
	}

	for i := 0; i < len(it.Keys); i++ {

		if key > it.Keys[i] {
			continue
		}

		if key == it.Keys[i] {
			if u64Allow(it.Attrs|attrs, ValueAttrSum) {
				it.Values[i] += value
				if attrs > 0 {
					it.Attrs |= attrs
				}
			} else {
				it.Values[i] = value
			}
		} else {
			it.Keys = append(append(it.Keys[:i], key), it.Keys[i:]...)
			it.Values = append(append(it.Values[:i], value), it.Values[i:]...)
		}

		return
	}

	it.Keys = append(it.Keys, key)
	it.Values = append(it.Values, value)

	if len(it.Keys) > CycleKeysMax {
		it.Keys = it.Keys[CycleKeysMax/10:]
		it.Values = it.Values[CycleKeysMax/10:]
	}
}

func NewCycleExportOptionsFromHttp(req *http.Request) *CycleExportOptions {

	var (
		opts = &CycleExportOptions{}
		ar   = req.URL.Query()
	)

	for k, v := range ar {
		if len(v) < 1 {
			continue
		}
		vs := v[len(v)-1]

		switch k {
		case "names":
			opts.Names = strings.Split(strings.ToLower(vs), ",")

		case "time_start":
			opts.TimeStart = parstInt(vs, 0)

		case "time_end":
			opts.TimeEnd = parstInt(vs, 0)

		case "time_unit":
			opts.TimeUnit = parstInt(vs, 0)

		case "time_zone":
			opts.TimeZone = parstInt(vs, 0)

		case "time_recent":
			tr := parstInt(vs, 0)
			if tr < 10 {
				tr = 10
			}
			opts.TimeEnd = time.Now().Unix()
			opts.TimeStart = opts.TimeEnd - tr
		}
	}

	return opts.reset()
}

func (it *CycleExportOptions) reset() *CycleExportOptions {

	tn := time.Now()

	if it.TimeStart > 0 {
		it.TimeStart = varUnixSecondFilter(it.TimeStart, 0, tn.Unix())
	}

	if it.TimeEnd > 0 {
		it.TimeEnd = varUnixSecondFilter(it.TimeEnd, 0, tn.Unix())
	}

	if (it.TimeStart + cycleExportOptionsTimeMax) < it.TimeEnd {
		it.TimeStart = it.TimeEnd - cycleExportOptionsTimeMax
	}

	if it.TimeZone < -11 {
		it.TimeZone = -11
	} else if it.TimeZone > 11 {
		it.TimeZone = 11
	}

	if it.TimeUnit == 0 {
		it.TimeUnit = 10
	} else if it.TimeUnit < 10 {
		it.TimeUnit = 10
	} else if it.TimeUnit > 86400 {
		it.TimeUnit = 86400
	}

	return it
}

func (it *CycleFeed) Export(opts *CycleExportOptions) *CycleFeed {

	cycleFeedMU.Lock()
	defer cycleFeedMU.Unlock()

	if opts == nil {
		opts = &CycleExportOptions{
			TimeUnit: 60,
		}
	}

	opts.reset()

	tzz := time.Local
	if opts.TimeZone != 0 {
		tzz = time.FixedZone("CST", int(opts.TimeZone*3600))
	}

	var (
		feed = &CycleFeed{
			Unit: opts.TimeUnit,
			Keys: []int64{},
		}
		timeStart = time.Unix(opts.TimeStart, 0).In(tzz)
	)

	if fix := int64(timeStart.Second()) % opts.TimeUnit; fix > 0 {
		opts.TimeStart -= fix
	}

	if opts.TimeUnit >= 60 {
		if fix := int64(timeStart.Minute()*60) % opts.TimeUnit; fix > 0 {
			opts.TimeStart -= fix
		}
	}

	if opts.TimeUnit >= 3600 {
		if fix := int64(timeStart.Hour()*3600) % opts.TimeUnit; fix > 0 {
			opts.TimeStart -= fix
		}
	}

	if opts.TimeUnit >= 86400 {
		if fix := int64(timeStart.Day()*86400) % opts.TimeUnit; fix > 0 {
			opts.TimeStart -= fix
		}
	}

	for k := opts.TimeStart; k <= opts.TimeEnd; k += opts.TimeUnit {
		feed.Keys = append(feed.Keys, k)
	}

	if len(feed.Keys) == 0 {
		return feed
	}

	for _, v := range it.Items {

		if len(v.Keys) != len(v.Values) {
			continue
		}

		if len(opts.Names) > 0 && !arrayStringHas(opts.Names, v.Name) {
			continue
		}

		entry := &CycleEntry{
			Name:   v.Name,
			Values: make([]int64, len(feed.Keys)),
			Attrs:  v.Attrs,
		}

		for j, k1 := range v.Keys {

			if k1 < opts.TimeStart || k1 > opts.TimeEnd {
				continue
			}

			entry.set(feed.Keys, k1, v.Values[j], 0)
		}

		feed.Items = append(feed.Items, entry)
	}

	return feed
}
