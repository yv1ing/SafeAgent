package redact

import (
	"math"
	"net/netip"
	"strings"
	"unicode"
)

type Detector struct {
	ID       string
	Label    string
	Pattern  string
	Default  bool
	Secret   bool
	Group    int
	Validate func(text string, start, end int) bool
}

const credentialValue = `[^\s"'` + "`" + `,;&<>(){}\[\]\\]`

var Detectors = []Detector{
	{ID: "email", Label: "EMAIL", Pattern: `[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+`, Default: true, Validate: validEmail},
	{ID: "cn_mobile", Label: "PHONE", Pattern: `(?:\+86|0086)?1[3-9][0-9]{9}`, Default: true, Validate: validMobile},
	{ID: "cn_id", Label: "ID_NUMBER", Pattern: `[1-9][0-9]{16}[0-9Xx]`, Default: true, Validate: validIdentity},
	{ID: "bank_card", Label: "BANK_CARD", Pattern: `[0-9]{13,19}|[0-9]{4}(?:[ -][0-9]{4}){2,4}(?:[ -]?[0-9]{1,3})?`, Default: true, Validate: validBankCard},
	{ID: "aws_access_key", Label: "AWS_KEY", Pattern: `(?:AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16}`, Default: true, Secret: true},
	{ID: "aliyun_access_key", Label: "ALIYUN_KEY", Pattern: `LTAI[0-9A-Za-z]{12,20}`, Default: true, Secret: true},
	{ID: "tencent_secret_id", Label: "TENCENT_KEY", Pattern: `AKID[0-9A-Za-z]{32}`, Default: true, Secret: true},
	{ID: "github_token", Label: "GITHUB_TOKEN", Pattern: `(?:gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,255})`, Default: true, Secret: true},
	{ID: "anthropic_key", Label: "ANTHROPIC_KEY", Pattern: `sk-ant-[A-Za-z0-9_-]{20,}`, Default: true, Secret: true},
	{ID: "openai_key", Label: "OPENAI_KEY", Pattern: `sk-(?:proj|svcacct|admin)-[A-Za-z0-9_-]{20,}|sk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20}|sk-[A-Za-z0-9]{48}`, Default: true, Secret: true, Validate: wholeToken},
	{ID: "google_api_key", Label: "GOOGLE_KEY", Pattern: `AIza[0-9A-Za-z_-]{35}`, Default: true, Secret: true, Validate: wholeToken},
	{ID: "slack_token", Label: "SLACK_TOKEN", Pattern: `xox[abposr]-[0-9A-Za-z-]{10,250}`, Default: true, Secret: true, Validate: wholeToken},
	{ID: "stripe_key", Label: "STRIPE_KEY", Pattern: `(?:sk|rk)_live_[0-9A-Za-z]{24,99}`, Default: true, Secret: true, Validate: wholeToken},
	{ID: "jwt", Label: "JWT", Pattern: `eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`, Default: true, Secret: true},
	{ID: "pem_private_key", Label: "PRIVATE_KEY", Pattern: `-----BEGIN [A-Z ]*PRIVATE KEY-----(?:[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----|[A-Za-z0-9+/=\r\n]*)`, Default: true, Secret: true},
	{ID: "bearer_token", Label: "BEARER_TOKEN", Pattern: `(?i)\bbearer\s+([A-Za-z0-9._~+/-]{16,}=*)`, Default: true, Secret: true, Group: 1},
	{ID: "ipv4", Label: "IP_ADDRESS", Pattern: `(?:[0-9]{1,3}\.){3}[0-9]{1,3}`, Validate: validIPv4},
	{ID: "ipv6", Label: "IP_ADDRESS", Pattern: `(?:[0-9A-Fa-f]{0,4}:){2,7}[0-9A-Fa-f]{0,4}`, Validate: validIPv6},
	{ID: "credential_assignment", Label: "CREDENTIAL", Pattern: `(?i)\b(?:api[_-]?key|secret[_-]?key|access[_-]?token|password|passwd|auth[_-]?token)["']?\s*[:=]\s*["']?(` + credentialValue + `{12,})`, Secret: true, Group: 1},
	{ID: "high_entropy_token", Label: "SECRET", Pattern: `[A-Za-z0-9+/_-]{32,512}={0,2}`, Secret: true, Validate: highEntropy},
}

func lookupDetector(id string) (Detector, bool) {
	for _, detector := range Detectors {
		if detector.ID == id {
			return detector, true
		}
	}
	return Detector{}, false
}

const (
	digits       = "0123456789"
	wordBytes    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	emailBefore  = wordBytes + "@.-"
	emailAfter   = wordBytes + "-"
	fileSuffixes = "png jpg jpeg gif svg webp css js json ts md html ico woff ttf pdf"
)

var provinceCodes = map[string]bool{"11": true, "12": true, "13": true, "14": true, "15": true, "21": true, "22": true, "23": true, "31": true, "32": true, "33": true, "34": true, "35": true, "36": true, "37": true, "41": true, "42": true, "43": true, "44": true, "45": true, "46": true, "50": true, "51": true, "52": true, "53": true, "54": true, "61": true, "62": true, "63": true, "64": true, "65": true, "71": true, "81": true, "82": true, "91": true}

// wholeToken rejects a match that is only part of a longer token.
func wholeToken(text string, start, end int) bool {
	return bounded(text, start, end, wordBytes+"-")
}

// highEntropy accepts a whole token that mixes letters and digits and whose characters carry at
// least minimumEntropy bits each, which separates random credentials from identifiers and words.
func highEntropy(text string, start, end int) bool {
	const minimumEntropy = 4.0
	if !bounded(text, start, end, wordBytes+"+/-") {
		return false
	}
	token := text[start:end]
	var letters, numbers bool
	counts := map[byte]int{}
	for index := 0; index < len(token); index++ {
		character := token[index]
		counts[character]++
		letters = letters || unicode.IsLetter(rune(character))
		numbers = numbers || isDigit(character)
	}
	if !letters || !numbers {
		return false
	}
	entropy := 0.0
	for _, count := range counts {
		probability := float64(count) / float64(len(token))
		entropy -= probability * math.Log2(probability)
	}
	return entropy >= minimumEntropy
}

func bounded(text string, start, end int, class string) bool {
	if start > 0 && strings.IndexByte(class, text[start-1]) >= 0 {
		return false
	}
	return end >= len(text) || strings.IndexByte(class, text[end]) < 0
}

func atoi(value string) int {
	result := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return -1
		}
		result = result*10 + int(character-'0')
	}
	return result
}

func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func validEmail(text string, start, end int) bool {
	if start > 0 && strings.IndexByte(emailBefore, text[start-1]) >= 0 {
		return false
	}
	if end < len(text) && strings.IndexByte(emailAfter, text[end]) >= 0 {
		return false
	}
	local, domain, _ := strings.Cut(text[start:end], "@")
	if local == "git" && end < len(text) && text[end] == ':' {
		return false
	}
	tld := domain[strings.LastIndexByte(domain, '.')+1:]
	if len(tld) < 2 {
		return false
	}
	for _, character := range tld {
		if !unicode.IsLetter(character) {
			return false
		}
	}
	return !strings.Contains(" "+fileSuffixes+" ", " "+strings.ToLower(tld)+" ")
}

func validMobile(text string, start, end int) bool {
	return bounded(text, start, end, digits)
}

func validIdentity(text string, start, end int) bool {
	if !bounded(text, start, end, digits) {
		return false
	}
	value := text[start:end]
	if !provinceCodes[value[:2]] {
		return false
	}
	year, month, day := atoi(value[6:10]), atoi(value[10:12]), atoi(value[12:14])
	if year < 1900 || year > 2200 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return false
	}
	return identityChecksum(value)
}

func daysInMonth(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

func identityChecksum(value string) bool {
	weights := [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	const codes = "10X98765432"
	sum := 0
	for index := range weights {
		sum += int(value[index]-'0') * weights[index]
	}
	return codes[sum%11] == value[17] || codes[sum%11] == value[17]-32
}

func validBankCard(text string, start, end int) bool {
	if !bounded(text, start, end, digits) {
		return false
	}
	number := strings.NewReplacer(" ", "", "-", "").Replace(text[start:end])
	if len(number) < 13 || len(number) > 19 || strings.Count(number, number[:1]) == len(number) {
		return false
	}
	return plausibleCardPrefix(number) && luhn(number)
}

// The issuer prefix and length gate keeps timestamps and other long digit runs that happen to pass
// the Luhn check from being treated as card numbers.
func plausibleCardPrefix(number string) bool {
	length := len(number)
	prefix2, prefix4 := atoi(number[:2]), atoi(number[:4])
	switch {
	case number[0] == '4':
		return length == 13 || length == 16 || length == 19
	case prefix2 == 34 || prefix2 == 37:
		return length == 15
	case prefix2 >= 51 && prefix2 <= 55, prefix4 >= 2221 && prefix4 <= 2720:
		return length == 16
	case prefix2 == 30 || prefix2 == 36 || prefix2 == 38:
		return length >= 14
	case prefix2 == 35 || prefix2 == 60 || prefix2 == 62 || prefix2 == 64 || prefix2 == 65 || prefix2 == 81:
		return length >= 16
	}
	return false
}

func luhn(number string) bool {
	sum := 0
	double := false
	for index := len(number) - 1; index >= 0; index-- {
		digit := int(number[index] - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return sum%10 == 0
}

func validIPv4(text string, start, end int) bool {
	if start > 0 && (text[start-1] == '.' || isDigit(text[start-1]) || text[start-1] == 'v' || text[start-1] == 'V') {
		return false
	}
	if end < len(text) && (text[end] == '.' || isDigit(text[end])) {
		return false
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimRight(text[max(0, start-9):start], " ")), "version") {
		return false
	}
	for _, part := range strings.Split(text[start:end], ".") {
		if len(part) > 1 && part[0] == '0' || atoi(part) > 255 {
			return false
		}
	}
	return true
}

func validIPv6(text string, start, end int) bool {
	if !bounded(text, start, end, wordBytes+":") {
		return false
	}
	address, err := netip.ParseAddr(text[start:end])
	return err == nil && address.Is6() && !address.IsUnspecified()
}
