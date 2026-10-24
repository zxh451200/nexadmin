package scim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"nexadmin/core/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (p *patchRequest) UnmarshalJSON(data []byte) error {
	type wire patchRequest
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*p = patchRequest(decoded)
	return nil
}

type protocolError struct {
	status       int
	kind, detail string
}

func (e *protocolError) Error() string  { return e.detail }
func invalid(kind, detail string) error { return &protocolError{400, kind, detail} }
func writeSCIMError(c *gin.Context, err error) {
	var pe *protocolError
	var re *response.Error
	switch {
	case errors.As(err, &pe):
		scimErrorType(c, pe.status, pe.kind, pe.detail)
	case errors.Is(err, gorm.ErrRecordNotFound):
		scimError(c, 404, "resource not found")
	case errors.Is(err, gorm.ErrDuplicatedKey):
		scimErrorType(c, 409, "uniqueness", "resource already exists")
	case errors.As(err, &re) && re.Status >= 400 && re.Status < 500:
		scimError(c, re.Status, re.Error())
	default:
		scimError(c, 500, "internal error")
	}
}

var equalityFilter = regexp.MustCompile(`(?i)^([a-z][a-z0-9]*)\s+eq\s+("(?:[^"\\\x00-\x1f]|\\.)*")$`)

func parseEquality(raw string, allowed map[string]string) (string, string, error) {
	match := equalityFilter.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return "", "", invalid("invalidFilter", "only supported attribute eq JSON string filters are allowed")
	}
	column, ok := allowed[strings.ToLower(match[1])]
	if !ok {
		return "", "", invalid("invalidFilter", "unsupported filter attribute")
	}
	var value string
	if json.Unmarshal([]byte(match[2]), &value) != nil {
		return "", "", invalid("invalidFilter", "invalid JSON string")
	}
	return column, value, nil
}
func pagination(c *gin.Context) (int, int, error) {
	start, err := strconv.Atoi(c.DefaultQuery("startIndex", "1"))
	if err != nil {
		return 0, 0, invalid("invalidValue", "invalid startIndex")
	}
	count, err := strconv.Atoi(c.DefaultQuery("count", "100"))
	if err != nil {
		return 0, 0, invalid("invalidValue", "invalid count")
	}
	if start < 1 {
		start = 1
	}
	if count < 0 {
		count = 0
	}
	if count > 200 {
		count = 200
	}
	return start, count, nil
}
func validatePatch(p patchRequest) error {
	if len(p.Schemas) != 1 || p.Schemas[0] != "urn:ietf:params:scim:api:messages:2.0:PatchOp" || len(p.Operations) == 0 {
		return invalid("invalidSyntax", "PatchOp schema and nonempty Operations are required")
	}
	for _, op := range p.Operations {
		switch strings.ToLower(op.Op) {
		case "add", "replace":
			if op.Value == nil {
				return invalid("invalidValue", "patch value is required")
			}
		case "remove":
			if strings.TrimSpace(op.Path) == "" {
				return invalid("noTarget", "remove requires a path")
			}
		default:
			return invalid("invalidSyntax", "unsupported patch operation")
		}
	}
	return nil
}

func patchUserResource(in *userResource, p patchRequest) error {
	if err := validatePatch(p); err != nil {
		return err
	}
	for _, op := range p.Operations {
		if op.Path == "" {
			fields, ok := op.Value.(map[string]any)
			if !ok || len(fields) == 0 {
				return invalid("invalidValue", "pathless patch requires an attribute object")
			}
			for path, value := range fields {
				if err := userAttribute(in, op.Op, path, value); err != nil {
					return err
				}
			}
		} else if err := userAttribute(in, op.Op, op.Path, op.Value); err != nil {
			return err
		}
	}
	return nil
}
func userAttribute(in *userResource, operation, path string, value any) error {
	remove := strings.EqualFold(operation, "remove")
	if remove && value != nil {
		return invalid("invalidValue", "remove must not include a value")
	}
	path = strings.TrimSpace(path)
	const userPrefix = "urn:ietf:params:scim:schemas:core:2.0:User:"
	if len(path) >= len(userPrefix) && strings.EqualFold(path[:len(userPrefix)], userPrefix) {
		path = path[len(userPrefix):]
	}
	if handled, err := patchContact(in, operation, path, value); handled {
		return err
	}
	path = strings.ToLower(path)
	switch path {
	case "username", "displayname", "externalid":
		if remove && path == "username" {
			return invalid("mutability", "userName is required")
		}
		v := ""
		if !remove {
			var ok bool
			v, ok = value.(string)
			if !ok {
				return invalid("invalidValue", "attribute must be a string")
			}
		}
		switch path {
		case "username":
			in.UserName = v
		case "displayname":
			in.DisplayName = v
		case "externalid":
			in.ExternalID = v
		}
	case "active":
		if remove {
			return invalid("mutability", "active cannot be removed")
		}
		v, ok := value.(bool)
		if !ok {
			return invalid("invalidValue", "active must be boolean")
		}
		in.Active = &v
	default:
		return invalid("invalidPath", fmt.Sprintf("unsupported user path %q", path))
	}
	return nil
}

func rejectConditional(c *gin.Context) bool {
	if c.GetHeader("If-Match") != "" || c.GetHeader("If-None-Match") != "" {
		scimErrorType(c, 400, "invalidValue", "conditional writes are not supported")
		return true
	}
	return false
}
