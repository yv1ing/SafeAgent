package console

import "testing"

func TestPasswordPolicyMatchesTheHasher(t *testing.T) {
	cases := map[string]bool{
		"Short1A":                  false,
		"lowercaseonlypassword123": false,
		"alllowercasepassword1234": false,
		"LongEnoughWithUpperAnd1":  true,
	}
	for password, expected := range cases {
		if ValidPassword(password) != expected {
			t.Fatalf("%q: expected %v", password, expected)
		}
	}
}
