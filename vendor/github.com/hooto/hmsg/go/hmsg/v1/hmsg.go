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

package hmsg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/htoml4g/htoml"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lessos/lessgo/types"
	"golang.org/x/text/language"
)

var (
	MsgIdRE            = regexp.MustCompile("^[a-f0-9]{16,32}$")
	MailTemplateNameRE = regexp.MustCompile("^[a-z]{1,1}[a-z0-9/]{2,32}$")
	mailConfPrefix     = "hmsg_mail_"
)

const (
	MsgActionPostOK      uint32 = 1 << 1
	MsgActionPostError   uint32 = 1 << 2
	MsgActionPostTimeout uint32 = 1 << 3
)

type HashSeed string

func NewMsgItem(args ...interface{}) *MsgItem {
	item := &MsgItem{}
	for _, arg := range args {
		switch arg.(type) {
		case HashSeed:
			item.Id = idhash.HashToHexString([]byte(arg.(HashSeed)), 16)
		}
	}
	if item.Id == "" {
		item.Id = idhash.RandHexString(16)
	}
	item.Created = uint32(time.Now().Unix())
	return item
}

func (it *MsgItem) Valid() error {

	if !MsgIdRE.MatchString(it.Id) {
		return errors.New("invalid msg id")
	}

	if it.ToUser == "" {
		return errors.New("user not found")
	}

	return nil
}

func (it *MsgItem) SentId() string {
	return uint32ToHexString(it.Created) + it.Id
}

func (it *MailTemplateEntry) Valid() error {

	name := types.NameIdentifier(it.Name)
	if err := name.Valid(); err != nil {
		return err
	}
	it.Name = string(name)

	return nil
}

func (it *MailTemplateEntry) Item(lang string) *MailTemplateLang {
	for _, v := range it.Items {
		if lang == v.Lang {
			return v
		}
	}
	return nil
}

func (it *MailTemplateLang) Equal(v *MailTemplateLang) bool {
	bs1, _ := proto.Marshal(it)
	bs2, _ := proto.Marshal(v)
	return bytes.Equal(bs1, bs2)
}

func (it *MailTemplateLang) Valid() error {

	it.Title = strings.TrimSpace(it.Title)
	it.Body = strings.TrimSpace(it.Body)

	if it.Title == "" {
		return errors.New("title not found")
	}

	if it.Body == "" {
		return errors.New("body not found")
	}

	if _, ok := MsgContentType_name[int32(it.BodyType)]; !ok {
		return errors.New("body-type not found")
	}

	it.Lang = strings.TrimSpace(it.Lang)
	if tag, err := language.Parse(it.Lang); err != nil {
		return err
	} else {
		it.Lang = tag.String()
	}

	return nil
}

type MailManagerConfig struct {
	Templates []*MailTemplateEntry `json:"templates" toml:"templates"`
}

type MailManager struct {
	mu            sync.RWMutex
	localeLangDef string
	items         map[string]*MailTemplateEntry
	tpls          map[string]*MailTemplateLang
}

func NewMailManager() *MailManager {
	return &MailManager{
		items: map[string]*MailTemplateEntry{},
		tpls:  map[string]*MailTemplateLang{},
	}
}

func (it *MailManager) LocaleLangSet(v string) {
	it.localeLangDef = strings.ToLower(strings.TrimSpace(v))
}

func (it *MailManager) TemplateLoad(args ...interface{}) error {
	it.mu.Lock()
	defer it.mu.Unlock()

	for _, arg := range args {

		switch arg.(type) {

		case string:
			if err := it.fsWalk(http.Dir(arg.(string)), "/"); err != nil {
				return err
			}
		}
	}

	return nil
}

func (it *MailManager) TemplateFlush(args ...interface{}) error {

	it.mu.Lock()
	defer it.mu.Unlock()

	for _, arg := range args {

		switch arg.(type) {

		case string:
			basedir := filepath.Clean(arg.(string))
			if st, err := os.Stat(basedir); err != nil {
				return err
			} else if !st.IsDir() {
				return errors.New("invalid dir")
			}
			for _, entry := range it.items {
				outpath := fmt.Sprintf("%s/%s%s.toml", basedir, mailConfPrefix, strings.Replace(entry.Name, "/", "_", -1))
				if err := htoml.EncodeToFile(entry, outpath, nil); err != nil {
					return err
				}
				hlog.Printf("info", "msg/mail template flush to %s ok", basedir)
			}
		}
	}
	return nil
}

func (it *MailManager) fsWalk(fs http.FileSystem, dir string) error {

	fp, err := fs.Open(dir)
	if err != nil {
		return err
	}
	defer fp.Close()

	st, err := fp.Stat()
	if err != nil {
		return err
	}

	if !st.IsDir() {
		if strings.HasPrefix(dir, mailConfPrefix) &&
			strings.HasSuffix(dir, ".toml") {
			var buf bytes.Buffer
			if _, err = io.Copy(&buf, fp); err != nil {
				return err
			}
			var tplEntry MailTemplateEntry
			if err := htoml.Decode(&tplEntry, buf.Bytes()); err == nil {
				it.TemplateSet(&tplEntry)
			}
		}
		return nil
	}

	nodes, err := fp.Readdir(-1)
	if err != nil {
		return err
	}

	for _, n := range nodes {

		if n.Name() == "." || n.Name() == ".." {
			continue
		}

		if err = it.fsWalk(fs, path.Join(dir, n.Name())); err != nil {
			return err
		}
	}

	return nil
}

func (it *MailManager) TemplateSet(set *MailTemplateEntry) error {

	if err := set.Valid(); err != nil {
		return err
	}

	it.mu.Lock()
	defer it.mu.Unlock()

	num := 0

	for _, v := range set.Items {

		if err := v.Valid(); err != nil {
			continue
		}

		entry, _ := it.items[set.Name]
		if entry == nil {
			entry = &MailTemplateEntry{
				Name: set.Name,
			}
			it.items[set.Name] = entry
		}

		item := entry.Item(v.Lang)
		if item != nil && (v.Version < item.Version || item.Equal(v)) {
			continue
		}

		num += 1

		item = &MailTemplateLang{
			Lang:     v.Lang,
			Title:    v.Title,
			Body:     v.Body,
			BodyType: v.BodyType,
			Version:  v.Version,
		}

		entry.Items = append(entry.Items, item)

		key := strings.ToLower(fmt.Sprintf("%s_%s", set.Name, v.Lang))

		it.tpls[key] = item
	}

	if num > 0 {
		hlog.Printf("info", "hmsg/mail-manager template set %d", num)
	}

	return nil
}

func (it *MailManager) TemplateRender(name string, lang string, data interface{}) (*MailTemplateLang, error) {
	it.mu.RLock()
	defer it.mu.RUnlock()

	if lang == "" {
		if it.localeLangDef != "" {
			lang = it.localeLangDef
		} else {
			lang = "en"
		}
	} else if !strings.Contains(lang, ",en") {
		lang += ",en"
	}
	langs := strings.Split(lang, ",")

	for _, v := range langs {

		//
		key := strings.ToLower(fmt.Sprintf("%s_%s", name, v))
		tpl, ok := it.tpls[key]
		if !ok {
			continue
		}

		//
		title, err := txtRender(tpl.Title, data)
		if err != nil {
			return nil, err
		}

		//
		body, err := txtRender(tpl.Body, data)
		if err != nil {
			return nil, err
		}

		return &MailTemplateLang{
			Title:    title,
			Body:     body,
			BodyType: tpl.BodyType,
		}, nil
	}

	return nil, errors.New("no template found")
}
