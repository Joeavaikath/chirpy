package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	hashedPass, err := bcrypt.GenerateFromPassword([]byte(password), 1)
	if err != nil {
		return password, err
	}

	return string(hashedPass), err

}

func CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn).UTC()),
		Subject:   userID.String(),
	})

	return jwtToken.SignedString([]byte(tokenSecret))
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&jwt.RegisteredClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(tokenSecret), nil
		})

	if err != nil {
		return uuid.Nil, err
	}

	userID, err := token.Claims.GetSubject()

	if err != nil {
		return uuid.Nil, err
	}

	return uuid.Parse(userID)
}

// Returns the JWT of the request
func GetBearerToken(headers http.Header) (string, error) {
	tokenString := headers.Get("Authorization")

	if tokenString == "" {
		return "", errors.New("auth header not present")
	}

	tokenStrings := strings.Split(tokenString, " ")

	if len(tokenString) < 2 {
		return "", errors.New("auth header malformed")
	}

	return tokenStrings[1], nil

}
