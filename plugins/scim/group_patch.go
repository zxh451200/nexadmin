package scim

import (
	"encoding/json"
	"strings"
	"time"

	"nexadmin/pkg/idgen"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *service) mutateGroup(c *gin.Context, mode string) {
	if rejectConditional(c) {
		return
	}
	var in groupResource
	var patch patchRequest
	if mode == "patch" {
		if c.ShouldBindJSON(&patch) != nil {
			writeSCIMError(c, invalid("invalidSyntax", "invalid patch"))
			return
		}
		if err := validatePatch(patch); err != nil {
			writeSCIMError(c, err)
			return
		}
	} else if mode != "delete" && c.ShouldBindJSON(&in) != nil {
		writeSCIMError(c, invalid("invalidSyntax", "invalid group"))
		return
	}
	var out groupResource
	err := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		local := *s
		local.db = tx
		var g group
		if mode == "create" {
			id, err := idgen.Next()
			if err != nil {
				return err
			}
			g = group{ID: id, TenantID: mustTenant(c), CreatedAt: time.Now().UTC()}
		} else {
			id, err := idgen.Parse(c.Param("id"))
			if err != nil || id <= 0 {
				return gorm.ErrRecordNotFound
			}
			if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, mustTenant(c)).First(&g).Error; err != nil {
				return err
			}
		}
		if mode == "delete" {
			if err := tx.Where("group_id = ? AND tenant_id = ?", g.ID, mustTenant(c)).Delete(&groupMember{}).Error; err != nil {
				return err
			}
			return tx.Where("id = ? AND tenant_id = ?", g.ID, mustTenant(c)).Delete(&group{}).Error
		}
		if mode == "patch" {
			current, err := local.groupResource(c, g)
			if err != nil {
				return err
			}
			in = current
			if err = patchGroupResource(&in, patch); err != nil {
				return err
			}
		}
		if strings.TrimSpace(in.DisplayName) == "" {
			return invalid("invalidValue", "displayName is required")
		}
		g.DisplayName, g.ExternalID, g.UpdatedAt = strings.TrimSpace(in.DisplayName), in.ExternalID, time.Now().UTC()
		var n int64
		q := tx.Model(&group{}).Where("tenant_id = ? AND id <> ?", mustTenant(c), g.ID)
		if g.ExternalID == "" {
			q = q.Where("display_name = ?", g.DisplayName)
		} else {
			q = q.Where("display_name = ? OR external_id = ?", g.DisplayName, g.ExternalID)
		}
		if err := q.Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return &protocolError{409, "uniqueness", "group already exists"}
		}
		if mode == "create" {
			insert := tx
			if g.ExternalID == "" {
				insert = insert.Omit("ExternalID")
			}
			if err := insert.Create(&g).Error; err != nil {
				return err
			}
		} else {
			var externalID any
			if g.ExternalID != "" {
				externalID = g.ExternalID
			}
			if err := tx.Model(&group{}).Where("id = ? AND tenant_id = ?", g.ID, mustTenant(c)).Updates(map[string]any{"display_name": g.DisplayName, "external_id": externalID, "updated_at": g.UpdatedAt}).Error; err != nil {
				return err
			}
		}
		if err := local.replaceMembers(c, g.ID, in.Members); err != nil {
			return err
		}
		var err error
		out, err = local.groupResource(c, g)
		return err
	})
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	if mode == "delete" {
		c.Status(204)
		return
	}
	code := 200
	if mode == "create" {
		code = 201
		c.Header("Location", strings.TrimRight(c.Request.URL.Path, "/")+"/"+out.ID)
	}
	c.JSON(code, out)
}

func patchGroupResource(in *groupResource, patch patchRequest) error {
	for _, op := range patch.Operations {
		if op.Path == "" {
			fields, ok := op.Value.(map[string]any)
			if !ok || len(fields) == 0 {
				return invalid("invalidValue", "pathless patch requires an attribute object")
			}
			for path, value := range fields {
				if err := groupAttribute(in, op.Op, path, value); err != nil {
					return err
				}
			}
		} else if err := groupAttribute(in, op.Op, op.Path, op.Value); err != nil {
			return err
		}
	}
	return nil
}

func groupAttribute(in *groupResource, operation, path string, value any) error {
	path = strings.TrimSpace(path)
	prefix := groupSchemaID + ":"
	if len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix) {
		path = path[len(prefix):]
	}
	op, path := strings.ToLower(operation), strings.ToLower(path)
	if op == "remove" && value != nil && path != "members" {
		return invalid("invalidValue", "remove value is only supported for members")
	}
	if strings.HasPrefix(path, "members[") {
		if op != "remove" || !strings.HasSuffix(path, "]") {
			return invalid("invalidPath", "member selector only supports remove")
		}
		_, id, err := parseEquality(path[len("members["):len(path)-1], map[string]string{"value": "value"})
		if err != nil {
			return invalid("invalidPath", "invalid member selector")
		}
		parsed, err := idgen.Parse(id)
		if err != nil || parsed <= 0 {
			return invalid("invalidValue", "invalid member id")
		}
		members := in.Members[:0]
		for _, m := range in.Members {
			if m.Value != parsed.String() {
				members = append(members, m)
			}
		}
		in.Members = members
		return nil
	}
	switch path {
	case "displayname", "externalid":
		if op == "remove" {
			if path == "displayname" {
				return invalid("mutability", "displayName is required")
			}
			in.ExternalID = ""
			return nil
		}
		v, ok := value.(string)
		if !ok {
			return invalid("invalidValue", "attribute must be a string")
		}
		if path == "displayname" {
			in.DisplayName = v
		} else {
			in.ExternalID = v
		}
	case "members":
		if op == "remove" && value == nil {
			in.Members = nil
			return nil
		}
		arr, ok := value.([]any)
		if !ok {
			return invalid("invalidValue", "members must be an array")
		}
		for _, item := range arr {
			member, ok := item.(map[string]any)
			if !ok {
				return invalid("invalidValue", "member must be an object")
			}
			id, ok := member["value"].(string)
			if !ok || id == "" {
				return invalid("invalidValue", "member value must be a nonempty string")
			}
			parsed, err := idgen.Parse(id)
			if err != nil || parsed <= 0 {
				return invalid("invalidValue", "invalid member id")
			}
			member["value"] = parsed.String()
			for k, v := range member {
				if k != "value" && k != "display" && k != "$ref" && k != "type" {
					return invalid("invalidPath", "unsupported member attribute")
				}
				if _, ok := v.(string); !ok {
					return invalid("invalidValue", "member attributes must be strings")
				}
			}
		}
		raw, err := json.Marshal(arr)
		if err != nil {
			return err
		}
		var decoded groupResource
		if err = json.Unmarshal([]byte(`{"members":`+string(raw)+`}`), &decoded); err != nil {
			return invalid("invalidValue", "invalid members")
		}
		switch op {
		case "replace":
			in.Members = decoded.Members
		case "add":
			in.Members = append(in.Members, decoded.Members...)
		case "remove":
			remove := make(map[string]bool)
			for _, m := range decoded.Members {
				remove[m.Value] = true
			}
			kept := in.Members[:0]
			for _, m := range in.Members {
				if !remove[m.Value] {
					kept = append(kept, m)
				}
			}
			in.Members = kept
		}
	default:
		return invalid("invalidPath", "unsupported group path")
	}
	return nil
}
