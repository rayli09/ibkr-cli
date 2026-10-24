package auth

import (
	"math/big"
	"testing"
)

// Test vectors in this file were generated with IBind's Python
// implementation (Voyz/ibind, ibind/oauth/oauth1a.py) on fixed inputs,
// so the Go port is verified against the reference implementation
// byte-for-byte. The RSA key is a throwaway test key; the DH prime is
// the sample prime printed in IBind's OAuth documentation. See
// docs/SPEC.md §8.

const testRSAPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAmQSYonTnDv9Q6wsItkRTYJ/mLV83PyXOiyuZOLS+NQtlbzV1
g1z74LOcihONb7NGEdWlTjFiN/CcopoYDvfzMxCM1G6AVgyGy4ivNA9SitHEXeT2
kYTpcbGPnyzxa64wwjq8Bb9WnQh86r8s1ueAc8PL0FsybFestIyRNn9UUMg0OGpu
8PWG/7u4tnzwEe7Xgwm2UEc/ny+t3LvL2OVTTaTMe/RZJaMEd2bq86M9exhqk3cG
NS6IVHodMsxd3+kdN/2PqJR7mOMK4hjB4vhQe67b2TQJspj6oAjAFl85lrNZVlSg
QZi8CWPilpV4sch5KjKbsfaQwsClw/+H1G5fmQIDAQABAoIBAAxp9otrf4P/IPdI
nbbSbo85ZF5MSg+fR2qRVBdpIRXtluzsQdyGknqlfQQorOyhzIP4OLZ8xCf0px3R
RuAOq+t5zWsTNz8uj1BQ26HrbPoNJZNWgXhxzNHn+AEPujBPC+6d/uYFIrI6X46t
3tCP1yMigH+mqRMSvn9RThtFPje6vh0XUPE96xw1bNuED7FnkzmZQRUcEy+5/1P5
K3wuLg/ewZUPmvJ1eoB1zixJXUMTchWFZ8IIdJnpFFq5bqfd9IvEGAN3K7pYgQxP
81XKSbC+ZV0xwLVbVmiZ/RRXqcbGBfkrzls8N/rbzpi4BNQ0bitkgPFeKQqhHe/o
0NrD+I8CgYEAup73kMHHLgm8kiOIU+kJ/Ppjvn6oYm/fa5x3Q8F/dtdLZM5FoPQb
mWimgHfBmYg4qOaEdK7RGKD8Je2bHNyxOpn9Yy8mjhZu0GNFsTXO44ry8D3GajRn
lT6cRDgce0hAQNYGAfyrM+AFnCh3uBj3qtsAxhgMJVJxEHONe96C3kMCgYEA0eeS
4qa92sW936EfjH82TrNp4q4SLrwRciL4Guu8IeggPxbzf/n6B9mCFrdiOlhKMrse
sQqUbIu6ddjoWqtpz9YiuSN/q1wyT0XCjxqfYBlgKtSVW85oKzIKx+ldCPbmfSWx
f7Ngwg6XdlIxOe34fJfRrDs96hKUz2u1ruzFovMCgYAwSf4JSPr/IKnYrrUR8VUS
TsAMhL2Rax1GvR/nOmR/Hcp2dSFzOncBxSCAUXyY+7Sdj7hqcOnOE6nrLoZae4io
fzwftA2n6ooWBfn8ls3H9yZRn8p9pL3A0oig5lwIUnO90yOO1BCAxdDtzVLdIijD
3xNeLs7FwYVcGmpbyQHX/QKBgC9okEJkyqio1NO3fnqrGMb5usaZ7j1j+MTApSzf
ZWR7Gx1kv6Z9/TzcoYcj8CmhW1DN9oaiDxfdD0YisYgZAxMQGFOccu9vKWXCKclM
iAaadJ/316wemGR5Lvor+PlXWoOVidt/1jC+PvNYaQned6scVx0BXGCLxdR7TVOu
4oyFAoGAN0z3m3UB+q4RTzQuW63XSMqmtI4Tj7ONVTGnVFw5x+MeE4gfy9xXMGpQ
f4MNMLw2bF379UIcPd1NfSQvZwavSTjHZHWa8+C3uTc5E6+aXZHu+ZxtnumO077r
VY6jWfzzobry1DX4bwLN0+V+c8+ErTRrImbhTVlAzaXHFCN0Y4Q=
-----END RSA PRIVATE KEY-----`

const (
	testPrime     = `00f6220dbb372eb7b734ef426c3dc68014ad46d51b9423073f40a5dc747b0d12aac75490534b114186e8dc303c3ec392e4853e2c340131ba72082ecaaf6bf5777781620a661e95768dfe3d86292408f5d8d3e1f3e90a8096d18e8b8c1c42e0c074bbff6b9c16983f60559927538be1cd668d79411111def4d2094754cbdc28b82270bf75fedd2ffcf3eef6538877393587112d33abc449bfb7d4be8effa4460baf65986adc30f3cf2c38e4f633a3052e209bd6eb7680734031a12a7c982efdb222ed001a9bce460d1f3d32a845b70c3d383c7645a9962028585945f0a8baf0d2d544aafcc400461cc77ba67ccd2340c22adbcd4709b3b1d723b0fdd9539c95a83`
	testATSBase64 = `foHN7SEMG31LxRgHt2dS5+paNpyAvM7Q+4FoC4icIxPZ6iha3k1Ls3fcpGc0NxX36ox2o7gsPZLv28q/x/8RwXaLBFotS4sjbTKi80F1Dzb2or6hXei3VkYPy90NIYB34qMRWLGLRRD+xn5EUq3alRMIYRUWR8NjibgIjmGHbQP5YFCDK0b/XX1EL+7ImkT5FxKHCIWPB0WUmysonOBuTjihXtYq6ZhrUTTpsDFlGD1882RQqrZJZxcp1ZtHOizl7oyA01VUHVkH166PGW+VksduTTShNW4jHMxOL6NAw7PWhquNTdPPtMxOh6hjGf5NRsHE+6v485CYG72atuC52A==`
	testPrepend   = `00112233445566778899aabbccddeeff`
	testDHRandom  = `a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90`
	testChallenge = `9fdad7e44f96758349215957e868dfd865f29fc3d1f96dad6c00876ed576f9c89a6542bb6257243e2fb483d47b3eadb5b3f4bfb0c5005b5ff15192485cd6b538c7216cbfd4543d08870d4644948ac873629dc711ab66ec1204a31cf63f2b7998d659ef70db56a864950ded21d4878388dad4c6fb5e0d9d52e776c2abe0fd97699ca76aa92bf24fb4311665403df39a241863f1a3f0adfa87d284871f7f90ebfcaa1dbe09c63ea20296269477a04c8fee60fcbad2ffa0ae541b7d82dce0563d8270793edfe1c348c2862a618cacfc1aa6c49140e12fe765b5aed4c2098fbe822326405044b077df58c5e5c356590c7d01df091b903e7972fa57ce86d5db8a00c`
	testDHResp    = `4d936bb5dc3a6c0c111570089136b595eceb5446333fbea1015a2c195c20ad05cf413a05d5f861c01d3fb6c639b46ea5a7d488e26cdd009d2246b92e636085d461ac41650333f22a892272d459622a65c624cf77a4d7149d40ebdaba04b3922b766584db291f8267f9d3c512cb63cb73c924f4c2a31cd98b66749383398d1fe1c3106cfc063ae3113e1ab614a806033b19dd45f99dd56b64b5235a0247b11a208adeb0cd8a8350498bf8f5933078b361aa8bdaee9d2f4a099641bea1f9994c50e6c83aaf6939055baffe2687f9f02d1e55c23dff07503731704cc210532941ef942eeafcb48fbd642c7e3a0bfab0d73383ecc07db536d3ba7b410c5f979880e`
	testLST       = `c6k0kdA92d2Lw3nUxdGb/hUwcHY=`
	testLSTSig    = `92720f61144d4cc90118cee8b3fbca8b88f535e6`
	testBase      = `00112233445566778899aabbccddeeffPOST&https%3A%2F%2Fapi.ibkr.com%2Fv1%2Fapi%2Foauth%2Flive_session_token&diffie_hellman_challenge%3D9fdad7e44f96758349215957e868dfd865f29fc3d1f96dad6c00876ed576f9c89a6542bb6257243e2fb483d47b3eadb5b3f4bfb0c5005b5ff15192485cd6b538c7216cbfd4543d08870d4644948ac873629dc711ab66ec1204a31cf63f2b7998d659ef70db56a864950ded21d4878388dad4c6fb5e0d9d52e776c2abe0fd97699ca76aa92bf24fb4311665403df39a241863f1a3f0adfa87d284871f7f90ebfcaa1dbe09c63ea20296269477a04c8fee60fcbad2ffa0ae541b7d82dce0563d8270793edfe1c348c2862a618cacfc1aa6c49140e12fe765b5aed4c2098fbe822326405044b077df58c5e5c356590c7d01df091b903e7972fa57ce86d5db8a00c%26oauth_consumer_key%3DTESTCONS%26oauth_nonce%3Dabc123nonce456XY%26oauth_signature_method%3DRSA-SHA256%26oauth_timestamp%3D1700000000%26oauth_token%3Dtok123`
	testRSASig    = `NdAvQlGXro5CPlkHMh01XsWwAWXk5zMOWxFZcL%2FrCNEe3KnkfUBfnks4sOLoyQzU%2FDK%2FSqOwpQrTBPSkI1dFxocKq5tN167yFzja1kfqzYNk4TSrAzbmuF0yU2Z8KX80dzZH5bcPnFkHuLq4do8Dx92tixZydBgi%2FTdrhK19K2MvdOiViiPoU2UExpSQkCkUk%2B%2F6%2FbTfrEOJF%2Bh0lz%2BqyvN%2BpTqQFHqBTmHv8u6B0wrrWIi1gVrTePqmubFVpqyz21OtaOPOiCE50lHvXeX6FR%2BNYFllxRP5vgGZAdtasy%2FwTepX%2FMlwzAAAGlTB4%2BEHh9lugGCwBFXvsSeCl0SuJg%3D%3D`
	testHMACSig   = `G%2F054hssjk2V7cz9Juri9xGmAyDRlpnToAMm%2F1R5Hxg%3D`
	testAuthHdr   = `OAuth realm="limited_poa", diffie_hellman_challenge="9fdad7e44f96758349215957e868dfd865f29fc3d1f96dad6c00876ed576f9c89a6542bb6257243e2fb483d47b3eadb5b3f4bfb0c5005b5ff15192485cd6b538c7216cbfd4543d08870d4644948ac873629dc711ab66ec1204a31cf63f2b7998d659ef70db56a864950ded21d4878388dad4c6fb5e0d9d52e776c2abe0fd97699ca76aa92bf24fb4311665403df39a241863f1a3f0adfa87d284871f7f90ebfcaa1dbe09c63ea20296269477a04c8fee60fcbad2ffa0ae541b7d82dce0563d8270793edfe1c348c2862a618cacfc1aa6c49140e12fe765b5aed4c2098fbe822326405044b077df58c5e5c356590c7d01df091b903e7972fa57ce86d5db8a00c", oauth_consumer_key="TESTCONS", oauth_nonce="abc123nonce456XY", oauth_signature="NdAvQlGXro5CPlkHMh01XsWwAWXk5zMOWxFZcL%2FrCNEe3KnkfUBfnks4sOLoyQzU%2FDK%2FSqOwpQrTBPSkI1dFxocKq5tN167yFzja1kfqzYNk4TSrAzbmuF0yU2Z8KX80dzZH5bcPnFkHuLq4do8Dx92tixZydBgi%2FTdrhK19K2MvdOiViiPoU2UExpSQkCkUk%2B%2F6%2FbTfrEOJF%2Bh0lz%2BqyvN%2BpTqQFHqBTmHv8u6B0wrrWIi1gVrTePqmubFVpqyz21OtaOPOiCE50lHvXeX6FR%2BNYFllxRP5vgGZAdtasy%2FwTepX%2FMlwzAAAGlTB4%2BEHh9lugGCwBFXvsSeCl0SuJg%3D%3D", oauth_signature_method="RSA-SHA256", oauth_timestamp="1700000000", oauth_token="tok123"`
)

func TestPrependVector(t *testing.T) {
	key, err := ParseRSAPrivateKey([]byte(testRSAPEM))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Prepend(testATSBase64, key)
	if err != nil {
		t.Fatal(err)
	}
	if got != testPrepend {
		t.Errorf("prepend = %s, want %s", got, testPrepend)
	}
}

func TestDHChallengeVector(t *testing.T) {
	got, err := DHChallenge(testPrime, testDHRandom, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != testChallenge {
		t.Errorf("challenge = %s, want %s", got, testChallenge)
	}
}

func TestLiveSessionTokenVector(t *testing.T) {
	lst, err := ComputeLiveSessionToken(testPrime, testDHRandom, testDHResp, testPrepend)
	if err != nil {
		t.Fatal(err)
	}
	if lst != testLST {
		t.Errorf("lst = %s, want %s", lst, testLST)
	}
	if !ValidateLiveSessionToken(lst, testLSTSig, "TESTCONS") {
		t.Error("LST signature validation failed")
	}
	if ValidateLiveSessionToken(lst, testLSTSig, "WRONG") {
		t.Error("LST validation accepted wrong consumer key")
	}
}

func vectorParams() map[string]string {
	return map[string]string{
		"oauth_consumer_key":       "TESTCONS",
		"oauth_nonce":              "abc123nonce456XY",
		"oauth_signature_method":   "RSA-SHA256",
		"oauth_timestamp":          "1700000000",
		"oauth_token":              "tok123",
		"diffie_hellman_challenge": testChallenge,
	}
}

func TestBaseStringAndSignaturesVector(t *testing.T) {
	base := BaseString("POST", "https://api.ibkr.com/v1/api/oauth/live_session_token", vectorParams(), testPrepend)
	if base != testBase {
		t.Errorf("base string mismatch:\n got %s\nwant %s", base, testBase)
	}
	key, err := ParseRSAPrivateKey([]byte(testRSAPEM))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := RSASignSHA256(base, key)
	if err != nil {
		t.Fatal(err)
	}
	if sig != testRSASig {
		t.Errorf("rsa sig = %s, want %s", sig, testRSASig)
	}
	h, err := HMACSignSHA256(base, testLST)
	if err != nil {
		t.Fatal(err)
	}
	if h != testHMACSig {
		t.Errorf("hmac sig = %s, want %s", h, testHMACSig)
	}
}

func TestAuthorizationHeaderVector(t *testing.T) {
	params := vectorParams()
	params["oauth_signature"] = testRSASig
	got := AuthorizationHeader("limited_poa", params)
	if got != testAuthHdr {
		t.Errorf("header mismatch:\n got %s\nwant %s", got, testAuthHdr)
	}
}

func TestBigToByteArrayJavaSemantics(t *testing.T) {
	// 0xff has bit length 8 (multiple of 8) -> leading 0x00 byte.
	x, _ := new(big.Int).SetString("ff", 16)
	if b := bigToByteArray(x); len(b) != 2 || b[0] != 0 || b[1] != 0xff {
		t.Errorf("0xff -> %v", b)
	}
	// 0x7f has bit length 7 -> no leading zero.
	y, _ := new(big.Int).SetString("7f", 16)
	if b := bigToByteArray(y); len(b) != 1 || b[0] != 0x7f {
		t.Errorf("0x7f -> %v", b)
	}
}

func TestNonceShape(t *testing.T) {
	n, err := GenerateNonce()
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 16 {
		t.Errorf("nonce length = %d", len(n))
	}
}
