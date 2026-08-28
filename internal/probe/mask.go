package probe

// MaskKey 把密钥脱敏成「前 6 位 + **** + 后 4 位」；短串全遮。
// 报告里绝不出现完整 key——报告会被贴到群里、发给 agent。
func MaskKey(k string) string {
	if k == "" {
		return "(空)"
	}
	if len(k) <= 12 {
		return "****"
	}
	return k[:6] + "****" + k[len(k)-4:]
}
