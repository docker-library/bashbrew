package templatelib

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

func swapStringsFuncBoolArgsOrder(a func(string, string) bool) func(string, string) bool {
	return func(str1 string, str2 string) bool {
		return a(str2, str1)
	}
}

func stringsActionFactory(name string, actOnFirst bool, action func([]string, string) string) func(args ...interface{}) string {
	return func(args ...interface{}) string {
		if len(args) < 2 {
			panic(fmt.Sprintf(`%q requires at least two arguments`, name))
		}

		var str string
		if actOnFirst {
			str = args[0].(string)
			args = args[1:]
		} else {
			str = args[len(args)-1].(string)
			args = args[:len(args)-1]
		}

		strs := []string{}
		for _, val := range args {
			switch val.(type) {
			case string:
				strs = append(strs, val.(string))
			case []string:
				strs = append(strs, val.([]string)...)
			default:
				panic(fmt.Sprintf(`unexpected type %T in %q (%+v)`, val, name, val))
			}
		}

		return action(strs, str)
	}
}

func stringsModifierActionFactory(a func(string, string) string) func([]string, string) string {
	return func(strs []string, str string) string {
		for _, mod := range strs {
			str = a(str, mod)
		}
		return str
	}
}

var FuncMap = template.FuncMap{
	"hasPrefix": swapStringsFuncBoolArgsOrder(strings.HasPrefix),
	"hasSuffix": swapStringsFuncBoolArgsOrder(strings.HasSuffix),

	"json": func(v interface{}) (string, error) {
		j, err := json.Marshal(v)
		return string(j), err
	},
	"join":         stringsActionFactory("join", true, strings.Join),
	"trimPrefixes": stringsActionFactory("trimPrefixes", false, stringsModifierActionFactory(strings.TrimPrefix)),
	"trimSuffixes": stringsActionFactory("trimSuffixes", false, stringsModifierActionFactory(strings.TrimSuffix)),
	"replace": stringsActionFactory("replace", false, func(strs []string, str string) string {
		return strings.NewReplacer(strs...).Replace(str)
	}),
	"first": stringsActionFactory("first", true, func(strs []string, str string) string { return str }),
	"last":  stringsActionFactory("last", false, func(strs []string, str string) string { return str }),
}
