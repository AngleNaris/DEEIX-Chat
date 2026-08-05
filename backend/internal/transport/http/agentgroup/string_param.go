package agentgroup

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// stringParam 读取路由参数，空值视为非法。
func stringParam(c *gin.Context, name string) (string, error) {
	value := strings.TrimSpace(c.Param(name))
	if value == "" {
		return "", errors.New("empty param")
	}
	return value, nil
}
