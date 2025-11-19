package settings

import (
	"encoding/json"
	"fmt"

	pmsettings "printmaster/common/settings"
)

// ApplyPatch merges the override map onto the base settings struct and returns the result.
func ApplyPatch(base pmsettings.Settings, overrides map[string]interface{}) (pmsettings.Settings, error) {
	if len(overrides) == 0 {
		return base, nil
	}
	baseMap, err := settingsToMap(base)
	if err != nil {
		return base, err
	}
	merged := cloneMap(baseMap)
	mergeMaps(merged, overrides)
	result, err := mapToSettings(merged)
	if err != nil {
		return base, err
	}
	pmsettings.Sanitize(&result)
	return result, nil
}

// MergeOverrideMaps deep-merges the incoming patch into the existing overrides map.
func MergeOverrideMaps(existing, patch map[string]interface{}) map[string]interface{} {
	if existing == nil {
		existing = map[string]interface{}{}
	} else {
		existing = cloneMap(existing)
	}
	mergeMaps(existing, patch)
	return existing
}

// CleanOverrides removes override entries that match the base settings.
func CleanOverrides(base pmsettings.Settings, overrides map[string]interface{}) (map[string]interface{}, error) {
	if overrides == nil {
		return map[string]interface{}{}, nil
	}
	baseMap, err := settingsToMap(base)
	if err != nil {
		return nil, err
	}
	merged := cloneMap(baseMap)
	mergeMaps(merged, overrides)
	diff := diffMaps(baseMap, merged)
	return diff, nil
}

func settingsToMap(s pmsettings.Settings) (map[string]interface{}, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal settings: %w", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings map: %w", err)
	}
	return out, nil
}

func mapToSettings(m map[string]interface{}) (pmsettings.Settings, error) {
	if m == nil {
		return pmsettings.DefaultSettings(), nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return pmsettings.Settings{}, fmt.Errorf("failed to marshal settings map: %w", err)
	}
	var out pmsettings.Settings
	if err := json.Unmarshal(data, &out); err != nil {
		return pmsettings.Settings{}, fmt.Errorf("failed to decode settings: %w", err)
	}
	return out, nil
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		switch vv := v.(type) {
		case map[string]interface{}:
			dst[k] = cloneMap(vv)
		default:
			dst[k] = vv
		}
	}
	return dst
}

func mergeMaps(dst map[string]interface{}, src map[string]interface{}) map[string]interface{} {
	if dst == nil {
		dst = map[string]interface{}{}
	}
	for k, v := range src {
		if vm, ok := v.(map[string]interface{}); ok {
			child, _ := dst[k].(map[string]interface{})
			dst[k] = mergeMaps(child, vm)
			continue
		}
		dst[k] = v
	}
	return dst
}

func diffMaps(base, updated map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, updatedVal := range updated {
		baseVal, exists := base[key]
		updatedMap, updatedIsMap := updatedVal.(map[string]interface{})
		baseMap, baseIsMap := baseVal.(map[string]interface{})
		if updatedIsMap {
			if !baseIsMap {
				baseMap = map[string]interface{}{}
			}
			childDiff := diffMaps(baseMap, updatedMap)
			if len(childDiff) > 0 {
				result[key] = childDiff
			}
			continue
		}
		if !exists || !valuesEqual(baseVal, updatedVal) {
			result[key] = updatedVal
		}
	}
	return result
}

func valuesEqual(a, b interface{}) bool {
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}
