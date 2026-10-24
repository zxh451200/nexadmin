package scim

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Type and primary are derived from the single stored value, never persisted.
type contactDTO struct {
	Value   string `json:"value"`
	Type    string `json:"type"`
	Primary *bool  `json:"primary"`
}

func workContact(value string) contactDTO {
	primary := true
	return contactDTO{Value: value, Type: "work", Primary: &primary}
}

func (c *contactDTO) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return invalid("invalidValue", "contact must be an object")
	}
	var decoded contactDTO
	seen := make(map[string]bool)
	for key, raw := range fields {
		key = strings.ToLower(key)
		if seen[key] {
			return invalid("invalidValue", "duplicate contact attribute")
		}
		seen[key] = true
		if string(raw) == "null" {
			return invalid("invalidValue", "contact attributes cannot be null")
		}
		switch key {
		case "value":
			if json.Unmarshal(raw, &decoded.Value) != nil {
				return invalid("invalidValue", "contact value must be a string")
			}
		case "type":
			if json.Unmarshal(raw, &decoded.Type) != nil || !strings.EqualFold(decoded.Type, "work") {
				return invalid("invalidValue", "only work contact type is supported")
			}
		case "primary":
			if json.Unmarshal(raw, &decoded.Primary) != nil || decoded.Primary == nil || !*decoded.Primary {
				return invalid("invalidValue", "a single work contact must be primary")
			}
		default:
			return invalid("invalidValue", "unsupported contact attribute")
		}
	}
	if strings.TrimSpace(decoded.Value) == "" {
		return invalid("invalidValue", "contact requires a nonempty value")
	}
	*c = workContact(decoded.Value)
	return nil
}

func validateUserContacts(in *userResource) error {
	for _, contacts := range []*[]contactDTO{&in.Emails, &in.PhoneNumbers} {
		if len(*contacts) > 1 {
			return invalid("invalidValue", "only one email and one phone are supported")
		}
		for i, c := range *contacts {
			if strings.TrimSpace(c.Value) == "" || (c.Type != "" && !strings.EqualFold(c.Type, "work")) || (c.Primary != nil && !*c.Primary) {
				return invalid("invalidValue", "contact requires a nonempty work value and primary true")
			}
			(*contacts)[i] = workContact(c.Value)
		}
	}
	return nil
}

var contactPath = regexp.MustCompile(`(?i)^(emails|phonenumbers)(?:\[(.*)\])?(?:\.(value|type|primary))?$`)

func decodeContact(value any) (contactDTO, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return contactDTO{}, invalid("invalidValue", "invalid contact")
	}
	var c contactDTO
	err = json.Unmarshal(raw, &c)
	return c, err
}

func patchContact(in *userResource, operation, path string, value any) (bool, error) {
	match := contactPath.FindStringSubmatch(path)
	if match == nil {
		return false, nil
	}
	contacts := &in.Emails
	if strings.EqualFold(match[1], "phonenumbers") {
		contacts = &in.PhoneNumbers
	}
	op, field := strings.ToLower(operation), strings.ToLower(match[3])
	filtered := strings.Contains(path, "[")
	matches := len(*contacts) > 0
	if filtered {
		attr, expected, err := parseEquality(match[2], map[string]string{"type": "type", "value": "value"})
		if err != nil {
			return true, err
		}
		if attr == "type" {
			matches = matches && strings.EqualFold(expected, "work")
		} else {
			matches = matches && (*contacts)[0].Value == expected
		}
		// A selector must not implicitly create a different contact.
		if !matches && op == "add" {
			if attr != "type" || !strings.EqualFold(expected, "work") || len(*contacts) != 0 {
				return true, invalid("noTarget", "contact selector has no matching target")
			}
		}
		if !matches && op != "add" {
			return true, invalid("noTarget", "contact selector has no matching target")
		}
	}
	if op == "remove" {
		if !matches {
			return true, invalid("noTarget", "contact does not exist")
		}
		switch field {
		case "", "value":
			*contacts = nil
			return true, nil
		default:
			return true, invalid("mutability", "work type and primary true cannot be removed")
		}
	}
	if field != "" {
		if !matches && (op != "add" || field != "value") {
			return true, invalid("noTarget", "contact value must exist before updating metadata")
		}
		var next contactDTO
		switch field {
		case "value":
			v, ok := value.(string)
			if !ok || strings.TrimSpace(v) == "" {
				return true, invalid("invalidValue", "contact value must be a nonempty string")
			}
			next = workContact(v)
		case "type":
			v, ok := value.(string)
			if !ok || !strings.EqualFold(v, "work") {
				return true, invalid("invalidValue", "only work contact type is supported")
			}
			next = workContact((*contacts)[0].Value)
		case "primary":
			v, ok := value.(bool)
			if !ok || !v {
				return true, invalid("invalidValue", "a single work contact must be primary")
			}
			next = workContact((*contacts)[0].Value)
		}
		if !filtered && op == "add" && matches && next.Value != (*contacts)[0].Value {
			return true, invalid("invalidValue", "adding a different contact exceeds single-value capacity")
		}
		*contacts = []contactDTO{next}
		return true, nil
	}
	var next []contactDTO
	if filtered {
		c, err := decodeContact(value)
		if err != nil {
			return true, err
		}
		next = []contactDTO{c}
	} else {
		arr, ok := value.([]any)
		if !ok || len(arr) > 1 {
			return true, invalid("invalidValue", "contact attribute must be an array with at most one element")
		}
		for _, item := range arr {
			c, err := decodeContact(item)
			if err != nil {
				return true, err
			}
			next = append(next, c)
		}
	}
	if op == "add" && !filtered {
		if len(next) == 0 {
			return true, nil
		}
		if matches && (*contacts)[0].Value != next[0].Value {
			return true, invalid("invalidValue", "adding a different contact exceeds single-value capacity")
		}
	}
	*contacts = next
	return true, nil
}
