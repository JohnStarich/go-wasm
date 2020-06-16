package interop

import "syscall/js"

var jsObject = js.Global().Get("Object")

func SliceFromStrings(strs []string) js.Value {
	var values []interface{}
	for _, s := range strs {
		values = append(values, s)
	}
	return js.ValueOf(values)
}

func SliceFromJSValues(args []js.Value) []interface{} {
	var values []interface{}
	for _, arg := range args {
		values = append(values, arg)
	}
	return values
}

func Keys(value js.Value) []string {
	jsKeys := jsObject.Call("keys", value)
	length := jsKeys.Length()
	var keys []string
	for i := 0; i < length; i++ {
		keys = append(keys, jsKeys.Index(i).String())
	}
	return keys
}

func Entries(value js.Value) map[string]js.Value {
	entries := make(map[string]js.Value)
	for _, key := range Keys(value) {
		entries[key] = value.Get(key)
	}
	return entries
}

func StringMap(m map[string]string) js.Value {
	jsValue := make(map[string]interface{})
	for key, value := range m {
		jsValue[key] = value
	}
	return js.ValueOf(jsValue)
}