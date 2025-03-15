package fiber

import (
	"anthropic-openai-api-proxy/type/response"
	"errors"
	"github.com/bsthun/gut"
	"github.com/gofiber/fiber/v2"
)

func HandleError(c *fiber.Ctx, err error) error {
	// * case of *fiber.Error
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return c.Status(fiberErr.Code).JSON(response.ErrorResponse{
			Success: gut.Ptr(false),
			Message: &fiberErr.Message,
		})
	}

	// * case of ErrorInstance
	var respErr *gut.ErrorInstance
	if errors.As(err, &respErr) {
		if respErr.Errors[0].Err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(&response.ErrorResponse{
				Success: gut.Ptr(false),
				Code:    &respErr.Errors[0].Code,
				Message: &respErr.Errors[0].Message,
				Error:   gut.Ptr(respErr.Errors[0].Err.Error()),
			})
		}
		return c.Status(fiber.StatusBadRequest).JSON(&response.ErrorResponse{
			Success: gut.Ptr(false),
			Code:    &respErr.Errors[0].Code,
			Message: &respErr.Errors[0].Message,
			Error:   nil,
		})
	}

	return c.Status(fiber.StatusInternalServerError).JSON(&response.ErrorResponse{
		Success: gut.Ptr(false),
		Code:    gut.Ptr("UNKNOWN_SERVER_ERROR"),
		Message: gut.Ptr("Unknown server error"),
		Error:   gut.Ptr(err.Error()),
	})
}
