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

package inapi

import "testing"

func TestValidateSpecConfigField(t *testing.T) {
	dbField := func(name, ftype string) *AppSpecConfigItem {
		return &AppSpecConfigItem{Name: name, Type: ftype}
	}

	tests := []struct {
		name    string
		field   *AppSpecConfigItem
		wantErr bool
	}{
		// flat fields
		{"flat string ok", dbField("db_name", SpecFieldTypeString), false},
		{"flat empty type ok", dbField("db_name", SpecFieldTypeUnspec), false},
		{"nil field", nil, true},
		{"empty name", &AppSpecConfigItem{Type: SpecFieldTypeString}, true},

		// group
		{"group ok", &AppSpecConfigItem{
			Name: "replica", Type: SpecFieldTypeGroup,
			Items: []*AppSpecConfigItem{
				{Name: "host", Type: SpecFieldTypeString},
				{Name: "port", Type: SpecFieldTypeString},
			},
		}, false},
		{"group without items", &AppSpecConfigItem{
			Name: "replica", Type: SpecFieldTypeGroup,
		}, true},

		// array_group
		{"array_group ok", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
			Items: []*AppSpecConfigItem{
				{Name: "db_name", Type: SpecFieldTypeString},
				{Name: "db_user", Type: SpecFieldTypeString},
				{Name: "db_auth", Type: SpecFieldTypeString, AutoFill: "hexstr_32"},
			},
		}, false},
		{"array_group without items", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
		}, true},
		{"array_group without key_item", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup,
			Items: []*AppSpecConfigItem{{Name: "db_name"}},
		}, true},
		{"array_group key_item not matching", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "missing",
			Items: []*AppSpecConfigItem{{Name: "db_name"}},
		}, true},

		// sibling name uniqueness
		{"duplicate sub-item name", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
			Items: []*AppSpecConfigItem{
				{Name: "db_name"},
				{Name: "db_name"},
			},
		}, true},
		{"sub-item empty name", &AppSpecConfigItem{
			Name: "replica", Type: SpecFieldTypeGroup,
			Items: []*AppSpecConfigItem{{Name: ""}},
		}, true},

		// unique scope
		{"valid unique=app on child", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
			Items: []*AppSpecConfigItem{
				{Name: "db_name"},
				{Name: "db_user", Unique: SpecConfigUniqueApp},
			},
		}, false},
		{"valid unique=array_group on child", &AppSpecConfigItem{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
			Items: []*AppSpecConfigItem{
				{Name: "db_name"},
				{Name: "db_user", Unique: SpecConfigUniqueArrayGroup},
			},
		}, false},
		{"invalid unique scope", &AppSpecConfigItem{
			Name: "db_user", Type: SpecFieldTypeString, Unique: "galaxy",
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSpecConfigField(tt.field)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSpecConfigField error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDeployConfigItems(t *testing.T) {
	spec := []*AppSpecConfigItem{
		{
			Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
			Items: []*AppSpecConfigItem{
				{Name: "db_name"},
				{Name: "db_auth", AutoFill: "hexstr_32"},
			},
		},
		{Name: "plain", Type: SpecFieldTypeString},
	}

	inst := func(key string) *AppDeployConfigItem {
		return &AppDeployConfigItem{
			Items: []*AppDeployConfigItem{
				{Name: "db_name", Value: key},
			},
		}
	}

	tests := []struct {
		name    string
		deploy  []*AppDeployConfigItem
		wantErr bool
	}{
		{
			"empty deploy ok",
			nil,
			false,
		},
		{
			"single instance ok",
			[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{inst("db1")}}},
			false,
		},
		{
			"multiple unique keys ok",
			[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
				inst("db1"), inst("db2"),
			}}},
			false,
		},
		{
			"duplicate key rejected",
			[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
				inst("db1"), inst("db1"),
			}}},
			true,
		},
		{
			"missing key field rejected",
			[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
				{Items: []*AppDeployConfigItem{{Name: "db_auth", Value: "x"}}},
			}}},
			true,
		},
		{
			"empty key value rejected",
			[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
				inst(""),
			}}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDeployConfigItems(spec, tt.deploy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDeployConfigItems error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDeployConfigUniqueness(t *testing.T) {
	inst := func(dbName, dbUser string) *AppDeployConfigItem {
		return &AppDeployConfigItem{Items: []*AppDeployConfigItem{
			{Name: "db_name", Value: dbName},
			{Name: "db_user", Value: dbUser},
		}}
	}

	t.Run("app_scope", func(t *testing.T) {
		// flat db_user (main) + array_group whose db_user child is unique="app";
		// values pool by name across main + every instance.
		spec := []*AppSpecConfigItem{
			{Name: "db_user", Type: SpecFieldTypeString},
			{
				Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
				Items: []*AppSpecConfigItem{
					{Name: "db_name"},
					{Name: "db_user", Unique: SpecConfigUniqueApp},
				},
			},
		}

		tests := []struct {
			name    string
			deploy  []*AppDeployConfigItem
			wantErr bool
		}{
			{
				"distinct across main and instances",
				[]*AppDeployConfigItem{
					{Name: "db_user", Value: "mainuser"},
					{Name: "databases", Items: []*AppDeployConfigItem{
						inst("db1", "u1"), inst("db2", "u2"),
					}},
				},
				false,
			},
			{
				"instance dup with main rejected",
				[]*AppDeployConfigItem{
					{Name: "db_user", Value: "mainuser"},
					{Name: "databases", Items: []*AppDeployConfigItem{
						inst("db1", "mainuser"),
					}},
				},
				true,
			},
			{
				"instance dup across instances rejected",
				[]*AppDeployConfigItem{
					{Name: "db_user", Value: "mainuser"},
					{Name: "databases", Items: []*AppDeployConfigItem{
						inst("db1", "u1"), inst("db2", "u1"),
					}},
				},
				true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateDeployConfigItems(spec, tt.deploy)
				if (err != nil) != tt.wantErr {
					t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("array_group_scope", func(t *testing.T) {
		spec := []*AppSpecConfigItem{
			{
				Name: "databases", Type: SpecFieldTypeArrayGroup, KeyItem: "db_name",
				Items: []*AppSpecConfigItem{
					{Name: "db_name"},
					{Name: "db_user", Unique: SpecConfigUniqueArrayGroup},
				},
			},
		}

		tests := []struct {
			name    string
			deploy  []*AppDeployConfigItem
			wantErr bool
		}{
			{
				"distinct db_user ok",
				[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
					inst("db1", "u1"), inst("db2", "u2"),
				}}},
				false,
			},
			{
				"duplicate db_user rejected",
				[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
					inst("db1", "u1"), inst("db2", "u1"),
				}}},
				true,
			},
			{
				"empty db_user values not treated as duplicates",
				[]*AppDeployConfigItem{{Name: "databases", Items: []*AppDeployConfigItem{
					inst("db1", ""), inst("db2", ""),
				}}},
				false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateDeployConfigItems(spec, tt.deploy)
				if (err != nil) != tt.wantErr {
					t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})
}
