package session

import "github.com/golang-jwt/jwt/v5"

func registered(jti string) jwt.RegisteredClaims { return jwt.RegisteredClaims{ID: jti} }
