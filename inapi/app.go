package inapi

import "fmt"

// Field returns the AppDeployConfigItem with the given name, or nil if not found
func (x *AppDeployConfigItem) Item(name string) *AppDeployConfigItem {
	if x != nil {
		for _, item := range x.Items {
			if item != nil && item.Name == name {
				return item
			}
		}
	}
	return nil
}

// // Value returns the value of the field with the given name
// func (x *AppDeployConfigItem) Value(name string) string {
// 	if field := x.Field(name); field != nil {
// 		return field.Value
// 	}
// 	return ""
// }

// // ValueOK returns the value of the field with the given name and a boolean indicating if it was found
// func (x *AppDeployConfigItem) ValueOK(name string) (string, bool) {
// 	if field := x.Field(name); field != nil {
// 		return field.Value, true
// 	}
// 	return "", false
// }

// ContainerName returns the container name for the app replica instance.
func (it *AppReplicaInstance) ContainerName() string {
	return fmt.Sprintf("app-%s-%04x", it.App.Id, it.Replica.Id)
}
