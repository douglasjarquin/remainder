package cursor

import "testing"

func TestMacConfigIdentityIgnoresNonStringHints(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  string
		valid bool
	}{
		{name: "native numeric userId with authId", body: `{"authInfo":{"userId":123,"authId":"synthetic-auth-id","email":"person@example.test"}}`, valid: true},
		{name: "email with nonstring hints", body: `{"authInfo":{"userId":123,"authId":{},"email":"person@example.test"}}`, valid: true},
		{name: "userId with nonstring email", body: `{"authInfo":{"userId":"synthetic-user","email":123}}`, valid: true},
		{name: "no string identity", body: `{"authInfo":{"userId":123,"authId":{},"email":null}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := readMacConfig(writeMacConfig(t, test.body))
			if (err == nil) != test.valid || (snapshot.fingerprint != "") != test.valid {
				t.Fatalf("valid=%t fingerprintPresent=%t error=%v", test.valid, snapshot.fingerprint != "", err)
			}
		})
	}
}
