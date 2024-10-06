package middleware

import (
	"blog-auth-server/models"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

var SecretKey = []byte("SecretKey")

type Middleware struct {
	ctx         context.Context
	redisClient *redis.Client
}

func NewMiddleware(ctx context.Context, redisClient *redis.Client) *Middleware {
	return &Middleware{
		ctx:         ctx,
		redisClient: redisClient,
	}
}

const (
	ErrorTokenParsing  = "Token parsing error"
	ErrorTokenExpired  = "Token has expired"
	ErrorInvalidToken  = "Invalid token"
	ErrorUnauthorized  = "Unauthorized"
	ErrorPermissionDen = "Permission denied"
)

func (uc *Middleware) UserMiddlewareHandler(c *fiber.Ctx) error {
	authorization := c.Get("Authorization")
	if len(authorization) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": ErrorUnauthorized})
	}
	tokenString := strings.TrimSpace(strings.Replace(authorization, "Bearer ", "", 1))

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("invalid signing method")
		}
		return SecretKey, nil
	})

	if err != nil {
		return handleJWTError(c, err)
	}
	if !token.Valid {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": ErrorInvalidToken})
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid claims"})
	}
	// 将claims存储到c.Locals，以便后续处理程序可以使用它们
	c.Locals("claims", claims)

	hash, ok := claims["hash"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Hash claim not found"})
	}
	value, err := uc.redisClient.Get(uc.ctx, hash).Result()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error retrieving permissions"})
	}
	var retrievedPermissions models.Permissions
	if err := json.Unmarshal([]byte(value), &retrievedPermissions); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error deserializing permissions"})
	}
	if retrievedPermissions.AdminFlag {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": ErrorPermissionDen})
	}

	return c.Next()

}

func handleJWTError(c *fiber.Ctx, err error) error {
	switch err.Error() {
	case "Missing or malformed JWT":
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing or malformed JWT"})
	default:
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid or expired JWT"})
	}
}

func (uc *Middleware) AdminMiddlewareHandler(c *fiber.Ctx) error {
	authorization := c.Get("Authorization")
	if authorization == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing Authorization header"})
	}
	tokenString := strings.TrimSpace(strings.Replace(authorization, "Bearer ", "", 1))

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("invalid signing method")
		}
		return SecretKey, nil
	})

	if err != nil {
		return handleJWTError(c, err)
	}
	if !token.Valid {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": ErrorInvalidToken})
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid claims"})
	}

	// 将claims存储到c.Locals，以便后续处理程序可以使用它们
	c.Locals("claims", claims)

	hash, ok := claims["hash"].(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Hash claim not found"})
	}
	value, err := uc.redisClient.Get(uc.ctx, hash).Result()
	if err != nil {
		log.Printf("Redis 获取权限失败: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error retrieving permissions"})
	}
	exists, err := uc.redisClient.Exists(uc.ctx, hash).Result()
	if err != nil {
		log.Printf("检查 Redis 键是否存在失败: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error checking permissions"})
	}
	if exists == 0 {
		log.Printf("Redis 中不存在权限数据: %s", hash)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Permissions not found"})
	}
	var retrievedPermissions models.Permissions
	if err := json.Unmarshal([]byte(value), &retrievedPermissions); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error deserializing permissions"})
	}
	if retrievedPermissions.AdminFlag {
		return c.Next()
	} else {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": ErrorPermissionDen})
	}

}
