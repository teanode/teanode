package datastruct

// UpdateMap deep-merges newValueMap into valueMap. Nil values in newValueMap
// delete the corresponding key from the result. Nested map[string]any values
// are merged recursively.
func UpdateMap(valueMap, newValueMap map[string]any) map[string]any {
	result := make(map[string]any, len(valueMap))
	for key, value := range valueMap {
		result[key] = value
	}
	for key, newValue := range newValueMap {
		if newValue == nil {
			delete(result, key)
			continue
		}
		existing, exists := result[key]
		if exists {
			result[key] = updateRecursively(existing, newValue)
		} else {
			result[key] = newValue
		}
	}
	return result
}

func updateRecursively(existing, newValue any) any {
	existingMap, existingIsMap := existing.(map[string]any)
	newMap, newIsMap := newValue.(map[string]any)
	if existingIsMap && newIsMap {
		return UpdateMap(existingMap, newMap)
	}
	return newValue
}
