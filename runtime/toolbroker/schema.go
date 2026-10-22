package toolbroker

// This deliberately bounded JSON Schema subset is checked at registration.
// Unknown assertions are rejected, never silently ignored. No remote refs.
import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"unicode/utf8"
)

func admitSchema(schema map[string]any, depth int) error {
	if depth > 32 {
		return fmt.Errorf("schema nesting exceeds 32")
	}
	for key, value := range schema {
		switch key {
		case "title", "description", "default", "examples", "$comment":
		case "type":
			names := []any{value}
			if list, ok := value.([]any); ok {
				names = list
			}
			if len(names) == 0 {
				return fmt.Errorf("empty schema type")
			}
			for _, name := range names {
				switch name {
				case "object", "array", "string", "number", "integer", "boolean", "null":
				default:
					return fmt.Errorf("unsupported schema type %v", name)
				}
			}
		case "properties":
			props, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("properties must be an object")
			}
			for _, prop := range props {
				nested, ok := prop.(map[string]any)
				if !ok {
					return fmt.Errorf("property must be a schema object")
				}
				if err := admitSchema(nested, depth+1); err != nil {
					return err
				}
			}
		case "items", "additionalProperties":
			if key == "additionalProperties" {
				if _, ok := value.(bool); ok {
					continue
				}
			}
			nested, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s must be a schema object", key)
			}
			if err := admitSchema(nested, depth+1); err != nil {
				return err
			}
		case "required":
			list, ok := value.([]any)
			if !ok {
				return fmt.Errorf("required must be an array")
			}
			for _, item := range list {
				if _, ok := item.(string); !ok {
					return fmt.Errorf("required names must be strings")
				}
			}
		case "enum":
			list, ok := value.([]any)
			if !ok || len(list) == 0 {
				return fmt.Errorf("enum must be nonempty")
			}
		case "const":
		case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties":
			n, ok := value.(float64)
			if !ok {
				return fmt.Errorf("%s must be numeric", key)
			}
			if key == "multipleOf" && n <= 0 {
				return fmt.Errorf("multipleOf must be positive")
			}
			if key == "minLength" || key == "maxLength" || key == "minItems" || key == "maxItems" || key == "minProperties" || key == "maxProperties" {
				if n < 0 || math.Trunc(n) != n {
					return fmt.Errorf("%s must be a nonnegative integer", key)
				}
			}
		case "pattern":
			pattern, ok := value.(string)
			if !ok {
				return fmt.Errorf("pattern must be a string")
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return err
			}
		case "uniqueItems":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("uniqueItems must be boolean")
			}
		default:
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	return nil
}

func validateSchemaValue(schema map[string]any, value any, path string) error {
	fail := func(rule string) error { return fmt.Errorf("%s violates %s", path, rule) }
	if declared, exists := schema["type"]; exists {
		names := []any{declared}
		if list, ok := declared.([]any); ok {
			names = list
		}
		valid := false
		for _, name := range names {
			if s, ok := name.(string); ok && matchesJSONType(value, s) {
				valid = true
			}
		}
		if !valid {
			return fail("type")
		}
	}
	if choices, ok := schema["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			if reflect.DeepEqual(value, choice) {
				found = true
			}
		}
		if !found {
			return fail("enum")
		}
	}
	if expected, exists := schema["const"]; exists && !reflect.DeepEqual(value, expected) {
		return fail("const")
	}
	bound := func(key string, n float64, less bool) error {
		if limit, ok := schema[key].(float64); ok {
			if (less && n < limit) || (!less && n > limit) {
				return fail(key)
			}
		}
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		if err := bound("minProperties", float64(len(v)), true); err != nil {
			return err
		}
		if err := bound("maxProperties", float64(len(v)), false); err != nil {
			return err
		}
		if required, ok := schema["required"].([]any); ok {
			for _, name := range required {
				if _, exists := v[name.(string)]; !exists {
					return fail("required " + name.(string))
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for name, item := range v {
			nested, ok := props[name].(map[string]any)
			if !ok {
				switch additional := schema["additionalProperties"].(type) {
				case bool:
					if !additional {
						return fail("additionalProperties")
					}
				case map[string]any:
					nested = additional
					ok = true
				}
			}
			if ok {
				if err := validateSchemaValue(nested, item, path+"."+name); err != nil {
					return err
				}
			}
		}
	case []any:
		if err := bound("minItems", float64(len(v)), true); err != nil {
			return err
		}
		if err := bound("maxItems", float64(len(v)), false); err != nil {
			return err
		}
		if unique, _ := schema["uniqueItems"].(bool); unique {
			seen := map[string]bool{}
			for _, item := range v {
				encoded, _ := json.Marshal(item)
				key := string(encoded)
				if seen[key] {
					return fail("uniqueItems")
				}
				seen[key] = true
			}
		}
		if nested, ok := schema["items"].(map[string]any); ok {
			for i, item := range v {
				if err := validateSchemaValue(nested, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case string:
		n := float64(utf8.RuneCountInString(v))
		if err := bound("minLength", n, true); err != nil {
			return err
		}
		if err := bound("maxLength", n, false); err != nil {
			return err
		}
		if pattern, ok := schema["pattern"].(string); ok {
			re := regexp.MustCompile(pattern)
			if !re.MatchString(v) {
				return fail("pattern")
			}
		}
	case float64:
		if err := bound("minimum", v, true); err != nil {
			return err
		}
		if err := bound("maximum", v, false); err != nil {
			return err
		}
		if n, ok := schema["exclusiveMinimum"].(float64); ok && v <= n {
			return fail("exclusiveMinimum")
		}
		if n, ok := schema["exclusiveMaximum"].(float64); ok && v >= n {
			return fail("exclusiveMaximum")
		}
		if n, ok := schema["multipleOf"].(float64); ok {
			// Compare canonical decimal values exactly. A float tolerance admits
			// invalid near-integers, and float division can overflow on valid input.
			value, validValue := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
			factor, validFactor := new(big.Rat).SetString(strconv.FormatFloat(n, 'g', -1, 64))
			if !validValue || !validFactor || factor.Sign() <= 0 || !new(big.Rat).Quo(value, factor).IsInt() {
				return fail("multipleOf")
			}
		}
	}
	return nil
}
