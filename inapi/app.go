package inapi

// Field returns the AppOptionField with the given name, or nil if not found
func (x *AppOption) Field(name string) *AppOptionField {
	if x == nil || x.Items == nil {
		return nil
	}
	for _, item := range x.Items {
		if item != nil && item.Name == name {
			return item
		}
	}
	return nil
}

// Value returns the value of the field with the given name
func (x *AppOption) Value(name string) string {
	if field := x.Field(name); field != nil {
		return field.Value
	}
	return ""
}

// ValueOK returns the value of the field with the given name and a boolean indicating if it was found
func (x *AppOption) ValueOK(name string) (string, bool) {
	if field := x.Field(name); field != nil {
		return field.Value, true
	}
	return "", false
}

// Field returns the AppDeployOptionField with the given name, or nil if not found
func (x *AppDeployOption) Field(name string) *AppDeployOptionField {
	if x == nil || x.Items == nil {
		return nil
	}
	for _, item := range x.Items {
		if item != nil && item.Name == name {
			return item
		}
	}
	return nil
}

// Value returns the value of the field with the given name
func (x *AppDeployOption) Value(name string) string {
	if field := x.Field(name); field != nil {
		return field.Value
	}
	return ""
}

// ValueOK returns the value of the field with the given name and a boolean indicating if it was found
func (x *AppDeployOption) ValueOK(name string) (string, bool) {
	if field := x.Field(name); field != nil {
		return field.Value, true
	}
	return "", false
}
