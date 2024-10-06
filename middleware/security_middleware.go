package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

type SecurityMiddleware struct{}

func NewSecurityMiddleware() *SecurityMiddleware {
	return &SecurityMiddleware{}
}

// RateLimiter 创建一个速率限制中间件
func (sm *SecurityMiddleware) RateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        5,               // 每个时间窗口的最大请求数
		Expiration: 1 * time.Minute, // 时间窗口大小
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP() // 使用IP地址作为限制键
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"message": "请求过于频繁，请稍后再试",
			})
		},
	})
}
