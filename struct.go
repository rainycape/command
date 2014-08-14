package command

import (
	"errors"
	"fmt"
	"reflect"
	"unicode"
	"unicode/utf8"
)

var (
	errNoPointer = errors.New("not a pointer")
	errNoStruct  = errors.New("not a struct")
)

func defaultFieldName(name string) string {
	var runes []rune
	for ii := 0; ii < len(name); {
		c, s := utf8.DecodeRuneInString(name[ii:])
		ii += s
		if unicode.IsUpper(c) {
			if len(runes) > 0 {
				runes = append(runes, '-')
			}
			c = unicode.ToLower(c)
		}
		runes = append(runes, c)
	}
	return string(runes)
}

type structVisitor func(name string, help string, field *reflect.StructField, val reflect.Value, ptr interface{}) error

func visitStruct(val reflect.Value, visitor structVisitor) error {
	if val.Kind() != reflect.Ptr {
		return errNoPointer
	}
	val = reflect.Indirect(val)
	if val.Kind() != reflect.Struct {
		return errNoStruct
	}
	typ := val.Type()
	for ii := 0; ii < typ.NumField(); ii++ {
		field := typ.Field(ii)
		fieldVal := val.Field(ii)
		ptr := fieldVal.Addr().Interface()
		name := defaultFieldName(field.Name)
		var help string
		if n := field.Tag.Get("name"); n != "" {
			name = n
		}
		if h := field.Tag.Get("help"); h != "" {
			help = h
		}
		if name == "" {
			return fmt.Errorf("no name provided for field %s in type %s", field.Name, typ)
		}
		if err := visitor(name, help, &field, fieldVal, ptr); err != nil {
			return err
		}
	}
	return nil
}
