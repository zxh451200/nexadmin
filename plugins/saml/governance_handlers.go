package saml

import (
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func (s *Service) listSourceVersions(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	rows, err := s.listVersions(c.Request.Context(), m.ID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rows)
}

func (s *Service) rollbackSource(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	var input struct {
		Version int    `json:"version"`
		Reason  string `json:"reason"`
	}
	if !response.BindJSON(c, &input) {
		return
	}
	start := time.Now()
	fresh, err := s.rollback(c.Request.Context(), m.ID, input.Version, input.Reason)
	s.audit(c, "rollback", m.ID.String(), start, map[string]any{"version": input.Version, "reason": input.Reason}, err)
	if err != nil {
		failDetail(c, err)
		return
	}
	response.OK(c, fresh)
}

func (s *Service) listSourceEvents(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	rows, err := s.listEvents(c.Request.Context(), m.ID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rows)
}

func (s *Service) listSourceSessions(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	rows, err := s.listSessions(c.Request.Context(), m.ID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, rows)
}

func (s *Service) revokeSourceSession(c *gin.Context) {
	m, ok := s.sourceOrFail(c)
	if !ok {
		return
	}
	sessionID, err := idgen.Parse(c.Param("sessionId"))
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	start := time.Now()
	err = s.revokeSession(c.Request.Context(), m.ID, sessionID)
	s.audit(c, "revoke_session", m.ID.String(), start, map[string]any{"sessionId": sessionID.String()}, err)
	if err != nil {
		failDetail(c, err)
		return
	}
	response.OK(c, nil)
}
