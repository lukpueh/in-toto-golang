package main

import (
  "os"
  "fmt"
  "sort"
  "regexp"
  "bytes"
  "encoding/json"
)


type A struct {
  B int `json:"b"`
  C string `json:"c"`
  D bool `json:"d"`
  E bool `json:"e"`
  F interface{} `json:"f"`
}

type X struct {
  Y A `json:"y"`
}

func _encode_canonical_string(s string) string  {
  re := regexp.MustCompile(`([\"])`)
  return fmt.Sprintf("\"%s\"", re.ReplaceAllString(s, "\\$1"))
}

func _encode_canonical(obj interface{}, result *bytes.Buffer) {
  switch objAsserted := obj.(type) {
    case string:
      result.WriteString(_encode_canonical_string(objAsserted))

    case bool:
      if objAsserted {
        result.WriteString("true")
      } else {
        result.WriteString("false")
      }

    case int:
      result.WriteString(string(objAsserted))

    case nil:
      result.WriteString("null")

    case []interface{}:
      result.WriteString("[")
      for i, val := range objAsserted {
        _encode_canonical(val, result)
        if i < (len(objAsserted) - 1) {
          result.WriteString(",")
        }
      }
      result.WriteString("]")

    // Assume that the keys are always strings
    case map[string]interface{}:
      result.WriteString("{")

      // Make a list of keys
      mapKeys := []string{}
      for key, _ := range objAsserted {
          mapKeys = append(mapKeys, key)
      }
      // Sort keys
      sort.Strings(mapKeys)

      // Canonicalize map
      for i, key := range mapKeys {
        _encode_canonical(key, result)
        result.WriteString(":")
        _encode_canonical(objAsserted[key], result)
        if i < (len(mapKeys) - 1) {
          result.WriteString(",")
        }
        i++
      }
      result.WriteString("}")
    default:
      fmt.Println(objAsserted, "is of a type I don't know how to handle")
  }

}


func encode_canonical(obj interface{}) {
  var result bytes.Buffer
  _encode_canonical(obj, &result)

  result.WriteTo(os.Stdout)
}


func main() {

  x := X{Y: A{B: 1, C: "yyy\n", D: true, E: false, F: nil}}
  data, _ := json.Marshal(x)


  var json_map interface{}

  json.Unmarshal(data, &json_map)

  encode_canonical(json_map)




}