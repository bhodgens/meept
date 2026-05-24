package configui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// GetKeypath resolves a dot-notation path against a config struct.
func GetKeypath(cfg *config.Config, path string) (string, error) {
	val, err := resolvePath(reflect.ValueOf(cfg), strings.Split(path, "."))
	if err != nil {
		return "", err
	}
	// Convert to string representation
	switch val.Kind() {
	case reflect.String:
		return val.String(), nil
	case reflect.Bool:
		return fmt.Sprintf("%v", val.Bool()), nil
	case reflect.Int, reflect.Int64:
		return fmt.Sprintf("%v", val.Int()), nil
	case reflect.Float64:
		return fmt.Sprintf("%v", val.Float()), nil
	case reflect.Slice:
		b, _ := json.Marshal(val.Interface())
		return string(b), nil
	default:
		b, _ := json.Marshal(val.Interface())
		return string(b), nil
	}
}

// SetKeypath sets a dot-notation path on a config struct.
func SetKeypath(cfg *config.Config, path string, value string) error {
	parts := strings.Split(path, ".")
	parent, err := resolvePath(reflect.ValueOf(cfg), parts[:len(parts)-1])
	if err != nil {
		return err
	}
	fieldName := parts[len(parts)-1]

	// Find field by JSON tag
	parentType := parent.Type()
	if parent.Kind() == reflect.Ptr {
		parent = parent.Elem()
		parentType = parent.Type()
	}
	for i := 0; i < parentType.NumField(); i++ {
		field := parentType.Field(i)
		tag := field.Tag.Get("json")
		tagName := strings.Split(tag, ",")[0]
		if tagName == fieldName {
			fv := parent.Field(i)
			switch fv.Kind() {
			case reflect.String:
				fv.SetString(value)
			case reflect.Bool:
				b, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid bool %q: %w", value, err)
				}
				fv.SetBool(b)
			case reflect.Int, reflect.Int64:
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid int %q: %w", value, err)
				}
				fv.SetInt(int64(n))
			case reflect.Float64:
				f, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return fmt.Errorf("invalid float %q: %w", value, err)
				}
				fv.SetFloat(f)
			default:
				return fmt.Errorf("unsupported type %s for field %s", fv.Kind(), fieldName)
			}
			return nil
		}
	}
	return fmt.Errorf("field %q not found", fieldName)
}

func resolvePath(v reflect.Value, parts []string) (reflect.Value, error) {
	for _, part := range parts {
		for v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("expected struct at %q, got %s", part, v.Kind())
		}
		t := v.Type()
		found := false
		for i := 0; i < t.NumField(); i++ {
			tag := t.Field(i).Tag.Get("json")
			tagName := strings.Split(tag, ",")[0]
			if tagName == part {
				v = v.Field(i)
				found = true
				break
			}
		}
		if !found {
			return reflect.Value{}, fmt.Errorf("field %q not found", part)
		}
	}
	return v, nil
}
