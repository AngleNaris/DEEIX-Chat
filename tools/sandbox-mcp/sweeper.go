package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sweepSharedExports 回收有效 scope 内过期的顶层普通导出文件，并回收无 live session
// 且已为空的老 scope 目录。绝不跟随 symlink，也不删除 mm-* 目录或其他用户目录。
// liveScopes 非 nil 时按 scope 判断是否存在活跃会话；存在活跃会话的 scope 不回收目录。
// 删除失败会聚合返回，便于上层告警（保留失败条目，下一轮继续尝试）。
func sweepSharedExports(root string, now time.Time, ttl time.Duration, liveScopes func(scope string) bool) error {
	if ttl <= 0 {
		return nil
	}
	var sweepErr error
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, scopeEntry := range entries {
		if !scopeEntry.IsDir() || scopeEntry.Type()&os.ModeSymlink != 0 || !validScopePattern.MatchString(scopeEntry.Name()) {
			continue
		}
		scopePath := filepath.Join(root, scopeEntry.Name())
		children, err := os.ReadDir(scopePath)
		if err != nil {
			sweepErr = errors.Join(sweepErr, fmt.Errorf("read scope %s: %w", scopeEntry.Name(), err))
			continue
		}
		nonEmpty := false
		for _, child := range children {
			if child.Type()&os.ModeSymlink != 0 || child.Name() == "" || strings.HasPrefix(child.Name(), "mm-") {
				nonEmpty = true
				continue
			}
			info, err := child.Info()
			if err != nil || now.Sub(info.ModTime()) <= ttl || !info.Mode().IsRegular() {
				nonEmpty = true
				continue
			}
			if err := os.Remove(filepath.Join(scopePath, child.Name())); err != nil && !os.IsNotExist(err) {
				sweepErr = errors.Join(sweepErr, fmt.Errorf("remove export %s: %w", child.Name(), err))
				nonEmpty = true
			}
		}
		if nonEmpty {
			continue
		}
		// 空目录回收：仅当无 live session，且目录本身也已超过 TTL（避免回收
		// 刚创建、尚未产生文件的活跃会话目录）。
		if liveScopes != nil && liveScopes(scopeEntry.Name()) {
			continue
		}
		scopeInfo, err := scopeEntry.Info()
		if err != nil {
			sweepErr = errors.Join(sweepErr, fmt.Errorf("stat scope %s: %w", scopeEntry.Name(), err))
			continue
		}
		if now.Sub(scopeInfo.ModTime()) > ttl {
			if err := os.Remove(scopePath); err != nil && !os.IsNotExist(err) {
				sweepErr = errors.Join(sweepErr, fmt.Errorf("remove empty scope %s: %w", scopeEntry.Name(), err))
			}
		}
	}
	return sweepErr
}
