package valueor

func Zero[Type any](value *Type) Type {
	if value != nil {
		return *value
	}
	var zero Type
	return zero
}
