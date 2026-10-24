package scim

import (
	"net/http"
	"strings"
	"time"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *service) listTokens(c *gin.Context) {
	var rows []token
	if err := s.db.WithContext(c.Request.Context()).Where("tenant_id = ?", mustTenant(c)).Order("created_at DESC").Find(&rows).Error; err != nil {
		response.Fail(c, response.Internal())
		return
	}
	out := make([]tokenDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, tokenDTO{ID: r.ID.String(), Name: r.Name, Revoked: r.Revoked, LastUsed: r.LastUsed, CreatedAt: r.CreatedAt})
	}
	response.OK(c, gin.H{"list": out, "total": len(out)})
}

func (s *service) createToken(c *gin.Context) {
	var in tokenInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	raw, err := newToken()
	if err != nil {
		response.Fail(c, response.Internal())
		return
	}
	id, err := idgen.Next()
	if err != nil {
		response.Fail(c, response.Internal())
		return
	}
	t := token{ID: id, TenantID: mustTenant(c), Name: strings.TrimSpace(in.Name), Hash: hashToken(raw), CreatedAt: time.Now().UTC()}
	if err := s.db.WithContext(c.Request.Context()).Create(&t).Error; err != nil {
		response.Fail(c, response.Internal())
		return
	}
	response.OK(c, gin.H{"token": raw, "item": tokenDTO{ID: id.String(), Name: t.Name, CreatedAt: t.CreatedAt}})
}

func (s *service) revokeToken(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	if err := s.db.WithContext(c.Request.Context()).Model(&token{}).Where("id = ? AND tenant_id = ?", id, mustTenant(c)).Update("revoked", true).Error; err != nil {
		response.Fail(c, response.Internal())
		return
	}
	response.OK(c, nil)
}

func mustTenant(c *gin.Context) idgen.ID {
	id, _ := tenant.From(c.Request.Context())
	return id
}

func (s *service) listUsers(c *gin.Context) {
	start, count, err := pagination(c)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	var rows []userRow
	q := s.db.WithContext(c.Request.Context()).Table("sys_user").Where("tenant_id = ? AND deleted_at IS NULL", mustTenant(c))
	if filter := strings.TrimSpace(c.Query("filter")); filter != "" {
		column, value, err := parseEquality(filter, map[string]string{"username": "username", "externalid": "external_id", "displayname": "nickname"})
		if err != nil {
			writeSCIMError(c, err)
			return
		}
		q = q.Where(column+" = ?", value)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		scimError(c, 500, "internal error")
		return
	}
	if count > 0 {
		if err := q.Order("id").Offset(start - 1).Limit(count).Find(&rows).Error; err != nil {
			scimError(c, 500, "internal error")
			return
		}
	}
	res := make([]userResource, 0, len(rows))
	for _, row := range rows {
		res = append(res, resource(row))
	}
	c.JSON(200, gin.H{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"}, "totalResults": total, "startIndex": start, "itemsPerPage": len(res), "Resources": res})
}

func (s *service) getUser(c *gin.Context) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		scimError(c, 404, "resource not found")
		return
	}
	row, err := s.findUser(c, id)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	c.JSON(200, resource(row))
}

func (s *service) createUser(c *gin.Context) {
	if rejectConditional(c) {
		return
	}
	var in userResource
	if err := c.ShouldBindJSON(&in); err != nil {
		scimErrorType(c, 400, "invalidValue", err.Error())
		return
	}
	if strings.TrimSpace(in.UserName) == "" {
		scimErrorType(c, 400, "invalidValue", "userName is required")
		return
	}
	if err := validateUserContacts(&in); err != nil {
		writeSCIMError(c, err)
		return
	}
	p, ok := core.CurrentUserProvisioner()
	if !ok {
		scimError(c, 503, "user provisioning unavailable")
		return
	}
	ctx := c.Request.Context()
	var n int64
	q := s.db.WithContext(ctx).Table("sys_user").Where("tenant_id = ? AND deleted_at IS NULL", mustTenant(c))
	if in.ExternalID == "" {
		q = q.Where("username = ?", in.UserName)
	} else {
		q = q.Where("username = ? OR external_id = ?", in.UserName, in.ExternalID)
	}
	if err := q.Count(&n).Error; err != nil {
		writeSCIMError(c, err)
		return
	}
	if n > 0 {
		scimErrorType(c, 409, "uniqueness", "user already exists")
		return
	}
	email := ""
	if len(in.Emails) > 0 {
		email = in.Emails[0].Value
	}
	phone := ""
	if len(in.PhoneNumbers) > 0 {
		phone = in.PhoneNumbers[0].Value
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	out, err := p.ProvisionUser(ctx, core.ProvisionUserInput{ExternalID: in.ExternalID, Username: in.UserName, Nickname: in.DisplayName, Email: email, Phone: phone, Status: status(active)})
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	row, err := s.findUser(c, out.ID)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, resource(row))
}

func (s *service) replaceUser(c *gin.Context) {
	s.updateUser(c, false)
}

func (s *service) patchUser(c *gin.Context) {
	s.updateUser(c, true)
}

func (s *service) updateUser(c *gin.Context, patch bool) {
	if rejectConditional(c) {
		return
	}
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		scimError(c, 404, "resource not found")
		return
	}
	row, err := s.findUser(c, id)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	in := resource(row)
	if patch {
		var p patchRequest
		if c.ShouldBindJSON(&p) != nil {
			scimError(c, 400, "invalid patch")
			return
		}
		if err := patchUserResource(&in, p); err != nil {
			writeSCIMError(c, err)
			return
		}
	} else {
		in = userResource{}
		if err := c.ShouldBindJSON(&in); err != nil {
			scimErrorType(c, 400, "invalidValue", err.Error())
			return
		}
	}
	if strings.TrimSpace(in.UserName) == "" {
		scimErrorType(c, 400, "invalidValue", "userName is required")
		return
	}
	if err := validateUserContacts(&in); err != nil {
		writeSCIMError(c, err)
		return
	}
	p, ok := core.CurrentUserProvisioner()
	if !ok {
		scimError(c, 503, "user provisioning unavailable")
		return
	}
	ctx := c.Request.Context()
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	email, phone := "", ""
	if len(in.Emails) > 0 {
		email = in.Emails[0].Value
	}
	if len(in.PhoneNumbers) > 0 {
		phone = in.PhoneNumbers[0].Value
	}
	_, err = p.UpdateProvisionedUser(ctx, id, core.ProvisionUserInput{ExternalID: in.ExternalID, ExternalIDSet: true, Username: in.UserName, Nickname: in.DisplayName, Email: email, Phone: phone, Status: status(active)})
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	row, err = s.findUser(c, id)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	c.JSON(200, resource(row))
}

func (s *service) deleteUser(c *gin.Context) {
	if rejectConditional(c) {
		return
	}
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		scimError(c, 404, "resource not found")
		return
	}
	p, ok := core.CurrentUserProvisioner()
	if !ok {
		scimError(c, 503, "user provisioning unavailable")
		return
	}
	if _, err := s.findUser(c, id); err != nil {
		writeSCIMError(c, err)
		return
	}
	if err := p.DisableProvisionedUser(c.Request.Context(), id); err != nil {
		writeSCIMError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *service) findUser(c *gin.Context, id idgen.ID) (userRow, error) {
	var row userRow
	err := s.db.WithContext(c.Request.Context()).Table("sys_user").Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", id, mustTenant(c)).First(&row).Error
	return row, err
}

func resource(r userRow) userResource {
	active := r.Status == "1"
	out := userResource{Schemas: []string{"urn:ietf:params:scim:schemas:core:2.0:User"}, ID: r.ID.String(), ExternalID: r.ExternalID, UserName: r.Username, DisplayName: r.Nickname, Active: &active, Meta: &struct {
		ResourceType string    `json:"resourceType"`
		Created      time.Time `json:"created"`
		LastModified time.Time `json:"lastModified"`
		Version      string    `json:"version,omitempty"`
	}{ResourceType: "User", Created: r.CreatedAt, LastModified: r.UpdatedAt}}
	if r.Email != "" {
		out.Emails = []contactDTO{workContact(r.Email)}
	}
	if r.Phone != "" {
		out.PhoneNumbers = []contactDTO{workContact(r.Phone)}
	}
	return out
}

func status(active bool) string {
	if active {
		return "1"
	}
	return "0"
}

func (s *service) listGroups(c *gin.Context) {
	start, count, err := pagination(c)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	q := s.db.WithContext(c.Request.Context()).Model(&group{}).Where("tenant_id = ?", mustTenant(c))
	if filter := strings.TrimSpace(c.Query("filter")); filter != "" {
		column, value, err := parseEquality(filter, map[string]string{"externalid": "external_id", "displayname": "display_name"})
		if err != nil {
			writeSCIMError(c, err)
			return
		}
		q = q.Where(column+" = ?", value)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		writeSCIMError(c, err)
		return
	}
	var rows []group
	if count > 0 {
		if err := q.Order("id").Offset(start - 1).Limit(count).Find(&rows).Error; err != nil {
			scimError(c, 500, "internal error")
			return
		}
	}
	out := make([]groupResource, 0, len(rows))
	for _, g := range rows {
		r, err := s.groupResource(c, g)
		if err != nil {
			scimError(c, 500, "internal error")
			return
		}
		out = append(out, r)
	}
	c.JSON(200, gin.H{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"}, "totalResults": total, "startIndex": start, "itemsPerPage": len(out), "Resources": out})
}

func (s *service) createGroup(c *gin.Context) {
	s.mutateGroup(c, "create")
}

func (s *service) getGroup(c *gin.Context) {
	g, err := s.findGroup(c)
	if err != nil {
		writeSCIMError(c, err)
		return
	}
	r, err := s.groupResource(c, g)
	if err != nil {
		scimError(c, 500, "internal error")
		return
	}
	c.JSON(200, r)
}

func (s *service) replaceGroup(c *gin.Context) {
	s.mutateGroup(c, "replace")
}

func (s *service) patchGroup(c *gin.Context) {
	s.mutateGroup(c, "patch")
}

func (s *service) deleteGroup(c *gin.Context) {
	s.mutateGroup(c, "delete")
}

func (s *service) findGroup(c *gin.Context) (group, error) {
	var g group
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id <= 0 {
		return g, gorm.ErrRecordNotFound
	}
	err = s.db.WithContext(c.Request.Context()).Where("id = ? AND tenant_id = ?", id, mustTenant(c)).First(&g).Error
	return g, err
}

func (s *service) replaceMembers(c *gin.Context, gid idgen.ID, members []struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}) error {
	tx := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ? AND tenant_id = ?", gid, mustTenant(c)).Delete(&groupMember{}).Error; err != nil {
			return err
		}
		seen := make(map[idgen.ID]bool)
		for _, m := range members {
			id, err := idgen.Parse(m.Value)
			if err != nil || id <= 0 {
				return invalid("invalidValue", "invalid member id")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			var n int64
			if err := tx.Table("sys_user").Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", id, mustTenant(c)).Count(&n).Error; err != nil {
				return err
			}
			if n != 1 {
				return invalid("invalidValue", "member not found in tenant")
			}
			if err := tx.Create(&groupMember{GroupID: gid, UserID: id, TenantID: mustTenant(c)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return tx
}

func (s *service) groupResource(c *gin.Context, g group) (groupResource, error) {
	var ms []groupMember
	if err := s.db.WithContext(c.Request.Context()).Where("group_id = ? AND tenant_id = ?", g.ID, mustTenant(c)).Find(&ms).Error; err != nil {
		return groupResource{}, err
	}
	r := groupResource{Schemas: []string{"urn:ietf:params:scim:schemas:core:2.0:Group"}, ID: g.ID.String(), ExternalID: g.ExternalID, DisplayName: g.DisplayName}
	for _, m := range ms {
		r.Members = append(r.Members, struct {
			Value   string `json:"value"`
			Display string `json:"display,omitempty"`
		}{Value: m.UserID.String()})
	}
	return r, nil
}
