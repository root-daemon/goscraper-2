package utils

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func HandleError(c *fiber.Ctx, err error) error {
	if err != nil && (strings.Contains(err.Error(), "invalid response format") ||
		strings.Contains(err.Error(), "invalid token format") ||
		strings.Contains(err.Error(), "server returned status 301") ||
		strings.Contains(err.Error(), "server returned status 302") ||
		strings.Contains(err.Error(), "server returned status 401") ||
		strings.Contains(err.Error(), "server returned status 403")) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"tokenInvalid": true,
			"error":        "Session expired or invalid",
			"status":       fiber.StatusUnauthorized,
		})
	}
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error":  err.Error(),
		"status": fiber.StatusInternalServerError,
	})
}
