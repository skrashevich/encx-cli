package encx

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexString decodes JSON string, number, bool, null, or object into a display string.
// The Encounter game engine sometimes returns sector/bonus answers as objects after a code is accepted.
type FlexString string

func (s FlexString) String() string {
	return string(s)
}

func (s *FlexString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}

	switch data[0] {
	case '"':
		var v string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*s = FlexString(v)
		return nil
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		*s = FlexString(answerFromObject(obj))
		return nil
	case 't', 'f':
		var v bool
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*s = FlexString(strconv.FormatBool(v))
		return nil
	default:
		var v json.Number
		if err := json.Unmarshal(data, &v); err == nil {
			*s = FlexString(v.String())
			return nil
		}
	}

	return fmt.Errorf("encx: flex string: unsupported JSON value %s", string(data))
}

func (s FlexString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

func answerFromObject(obj map[string]json.RawMessage) string {
	keys := []string{
		"Answ", "Answer", "AnswForm", "Text", "Value",
		"text", "value", "Login", "Code", "FormattedAnswer", "AnswerText",
	}
	for _, key := range keys {
		if s := rawToString(obj[key]); s != "" {
			return s
		}
	}
	for _, raw := range obj {
		if s := rawToString(raw); s != "" {
			return s
		}
	}
	return ""
}

func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}
