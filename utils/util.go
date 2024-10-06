package utils

import (
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	// Generate a salt with a cost factor of 10
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(password, hashedPassword string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// 此处可以优化，将两个方法合并，返回一个username的string
func IsValidEmail(userInput string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(userInput)
}

func IsValidPhone(userInput string) bool {
	// 以1开头，第二位是3-9，后面跟着9位数字
	phoneRegex := regexp.MustCompile(`^1[3-9]\d{9}$`)
	return phoneRegex.MatchString(userInput)
}
