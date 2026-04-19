// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
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

package inauth

import "sync"

type AccessKeyManager struct {
	mu    sync.RWMutex
	items map[string]*AccessKey
	roles map[string]*accessKeyManagerRole
}

type accessKeyManagerRole struct {
	permissions map[string]bool
}

func NewAccessKeyManager() *AccessKeyManager {
	return &AccessKeyManager{
		items: map[string]*AccessKey{},
		roles: map[string]*accessKeyManagerRole{},
	}
}

func (it *AccessKeyManager) Set(k *AccessKey) error {

	it.mu.Lock()
	defer it.mu.Unlock()

	if ak, ok := it.items[k.Id]; !ok || k != ak {
		it.items[k.Id] = k
	}

	return nil
}

func (it *AccessKeyManager) Del(id string) error {

	it.mu.Lock()
	defer it.mu.Unlock()

	delete(it.items, id)

	return nil
}

func (it *AccessKeyManager) Key(id string) *AccessKey {

	it.mu.RLock()
	defer it.mu.RUnlock()

	key, ok := it.items[id]
	if ok {
		return key
	}
	return nil
}

func (it *AccessKeyManager) Count() int {
	it.mu.Lock()
	defer it.mu.Unlock()
	return len(it.items)
}

func (it *AccessKeyManager) SetRole(r *Role) *AccessKeyManager {

	it.mu.Lock()
	defer it.mu.Unlock()

	role, ok := it.roles[r.Name]
	if !ok {
		role = &accessKeyManagerRole{
			permissions: map[string]bool{},
		}
		it.roles[r.Name] = role
	}

	for _, p := range r.Permissions {
		role.permissions[p] = true
	}

	return it
}
