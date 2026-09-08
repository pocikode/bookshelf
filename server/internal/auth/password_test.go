package auth

import "testing"

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("valid password was rejected")
	}
	if CheckPassword(hash, "wrong password") {
		t.Fatal("invalid password was accepted")
	}
}
