package scim

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	userSchemaID     = "urn:ietf:params:scim:schemas:core:2.0:User"
	groupSchemaID    = "urn:ietf:params:scim:schemas:core:2.0:Group"
	resourceSchemaID = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
)

func (s *service) providerConfig(c *gin.Context) {
	c.JSON(200, gin.H{
		"schemas":        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":          gin.H{"supported": true},
		"bulk":           gin.H{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":         gin.H{"supported": true, "maxResults": 200},
		"changePassword": gin.H{"supported": false},
		"sort":           gin.H{"supported": false},
		"meta":           discoveryMeta(c, "ServiceProviderConfig", ""),
		// UserProvisioner does not expose a conditional atomic update.
		"etag": gin.H{"supported": false},
		"authenticationSchemes": []gin.H{{
			"type": "oauthbearertoken", "name": "Bearer token",
			"description": "Tenant-scoped SCIM token issued by the administrator",
			"primary":     true,
		}},
	})
}

func scimResourceTypes() []gin.H {
	return []gin.H{
		{"schemas": []string{resourceSchemaID}, "id": "User", "name": "User", "endpoint": "/Users", "schema": userSchemaID, "schemaExtensions": []any{}},
		{"schemas": []string{resourceSchemaID}, "id": "Group", "name": "Group", "endpoint": "/Groups", "schema": groupSchemaID, "schemaExtensions": []any{}},
	}
}

func discoveryList(c *gin.Context, values []gin.H) {
	for _, item := range values {
		id, _ := item["id"].(string)
		kind := "Schema"
		if strings.Contains(c.FullPath(), "/ResourceTypes") {
			kind = "ResourceType"
		}
		item["meta"] = discoveryMeta(c, kind, id)
	}
	c.JSON(200, gin.H{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(values), "startIndex": 1, "itemsPerPage": len(values), "Resources": values})
}

func (s *service) resourceTypes(c *gin.Context) { discoveryList(c, scimResourceTypes()) }
func (s *service) resourceType(c *gin.Context) {
	for _, item := range scimResourceTypes() {
		if item["id"] == c.Param("id") {
			item["meta"] = discoveryMeta(c, "ResourceType", c.Param("id"))
			c.JSON(200, item)
			return
		}
	}
	scimError(c, 404, "resource type not found")
}

func scimAttribute(name, kind, mutability string, required, multi bool) gin.H {
	return gin.H{
		"name": name, "type": kind, "multiValued": multi, "required": required,
		"caseExact": false, "mutability": mutability, "returned": "default", "uniqueness": "none",
	}
}

func scimSchemas() []gin.H {
	userName := scimAttribute("userName", "string", "readWrite", true, false)
	userName["uniqueness"] = "server"
	attributes := []gin.H{
		scimAttribute("id", "string", "readOnly", false, false),
		scimAttribute("externalId", "string", "readWrite", false, false),
		userName,
		scimAttribute("displayName", "string", "readWrite", false, false),
		scimAttribute("active", "boolean", "readWrite", false, false),
	}
	for _, name := range []string{"emails", "phoneNumbers"} {
		item := scimAttribute(name, "complex", "readWrite", false, true)
		item["description"] = "One primary work contact is supported; omitted type means work and omitted primary means true."
		contactType := scimAttribute("type", "string", "readWrite", false, false)
		contactType["canonicalValues"] = []string{"work"}
		item["subAttributes"] = []gin.H{
			scimAttribute("value", "string", "readWrite", true, false),
			contactType,
			scimAttribute("primary", "boolean", "readWrite", false, false),
		}
		attributes = append(attributes, item)
	}
	members := scimAttribute("members", "complex", "readWrite", false, true)
	members["subAttributes"] = []gin.H{
		scimAttribute("value", "string", "immutable", true, false),
		scimAttribute("display", "string", "readOnly", false, false),
	}
	return []gin.H{
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			"id": userSchemaID, "name": "User", "description": "Provisioned user", "attributes": attributes},
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			"id": groupSchemaID, "name": "Group", "description": "SCIM group",
			"attributes": []gin.H{
				scimAttribute("id", "string", "readOnly", false, false),
				scimAttribute("externalId", "string", "readWrite", false, false),
				scimAttribute("displayName", "string", "readWrite", true, false),
				members,
			}},
	}
}

func (s *service) schemas(c *gin.Context) { discoveryList(c, scimSchemas()) }
func (s *service) schema(c *gin.Context) {
	for _, item := range scimSchemas() {
		if item["id"] == c.Param("id") {
			item["meta"] = discoveryMeta(c, "Schema", c.Param("id"))
			c.JSON(200, item)
			return
		}
	}
	scimError(c, 404, "schema not found")
}

// Use the mounted route prefix and a relative URL; never trust the request Host.
func discoveryMeta(c *gin.Context, kind, id string) gin.H {
	collection := kind
	switch kind {
	case "Schema":
		collection = "Schemas"
	case "ResourceType":
		collection = "ResourceTypes"
	}
	route := c.FullPath()
	index := strings.LastIndex(route, "/"+collection)
	location := "/" + collection
	if index >= 0 {
		location = route[:index] + location
	}
	if id != "" {
		location += "/" + url.PathEscape(id)
	}
	return gin.H{"resourceType": kind, "location": location}
}
