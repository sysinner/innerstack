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

package inutil

import (
	"encoding/json"
	"fmt"
	"os"
)

// JsonDecodeFromFile reads JSON data from a file and decodes it into the provided value.
// The value must be a pointer to a JSON-serializable type.
func JsonDecodeFromFile(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err // fmt.Errorf("[inutil.JsonDecodeFromFile] read file %s: %w", path, err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("[inutil.JsonDecodeFromFile] unmarshal json from %s: %w", path, err)
	}

	return nil
}

// JsonEncodeToFile encodes the provided value to JSON and writes it to a file.
// The value must be a JSON-serializable type.
func JsonEncodeToFile(path string, v interface{}, perm os.FileMode) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("[inutil.JsonEncodeToFile] marshal json: %w", err)
	}

	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("[inutil.JsonEncodeToFile] write file %s: %w", path, err)
	}

	return nil
}

// JsonEncodeToFileIndent encodes the provided value to JSON with indentation
// and writes it to a file. The value must be a JSON-serializable type.
func JsonEncodeToFileIndent(path string, v interface{}, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("[inutil.JsonEncodeToFileIndent] marshal json: %w", err)
	}

	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("[inutil.JsonEncodeToFileIndent] write file %s: %w", path, err)
	}

	return nil
}
