package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 测试向量来自 AWS 官方文档 “Signature Calculations for the Authorization Header: Transferring Payload in a Single Chunk”
// 与 “Authenticating Requests: Using Query Parameters”（examplebucket，us-east-1，2013-05-24T00:00:00Z）。
var (
	awsSigner = Signer{
		AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region: "us-east-1", Service: "s3",
	}
	awsTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
)

func newAWSReq(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func checkVector(t *testing.T, req *http.Request, payload, wantCanonical, wantStringToSign, wantSignature, wantAuth string) {
	t.Helper()
	awsSigner.SignRequest(req, payload, awsTime)
	q := req.URL.Query()
	canonical, sts, sig, _ := awsSigner.details(req, canonicalQuery(q), payload, awsTime)
	if canonical != wantCanonical {
		t.Errorf("canonical request mismatch\n got: %q\nwant: %q", canonical, wantCanonical)
	}
	if sts != wantStringToSign {
		t.Errorf("string to sign mismatch\n got: %q\nwant: %q", sts, wantStringToSign)
	}
	if sig != wantSignature {
		t.Errorf("signature mismatch\n got: %s\nwant: %s", sig, wantSignature)
	}
	if got := req.Header.Get("Authorization"); got != wantAuth {
		t.Errorf("Authorization mismatch\n got: %s\nwant: %s", got, wantAuth)
	}
}

func TestSigV4_GetObject(t *testing.T) {
	req := newAWSReq(t, "GET", "https://examplebucket.s3.amazonaws.com/test.txt")
	req.Header.Set("Range", "bytes=0-9")
	checkVector(t, req, emptyPayload,
		strings.Join([]string{
			"GET", "/test.txt", "",
			"host:examplebucket.s3.amazonaws.com",
			"range:bytes=0-9",
			"x-amz-content-sha256:" + emptyPayload,
			"x-amz-date:20130524T000000Z",
			"",
			"host;range;x-amz-content-sha256;x-amz-date",
			emptyPayload,
		}, "\n"),
		"AWS4-HMAC-SHA256\n20130524T000000Z\n20130524/us-east-1/s3/aws4_request\n7344ae5b7ee6c3e7e6b0fe0640412a37625d1fbfff95c48bbb2dc43964946972",
		"f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,"+
			"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,"+
			"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41")
}

func TestSigV4_PutObject(t *testing.T) {
	body := sha256.Sum256([]byte("Welcome to Amazon S3."))
	payload := hex.EncodeToString(body[:])
	if payload != "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072" {
		t.Fatalf("payload hash %s", payload)
	}
	req := newAWSReq(t, "PUT", "https://examplebucket.s3.amazonaws.com/test$file.text")
	req.Header.Set("Date", "Fri, 24 May 2013 00:00:00 GMT")
	req.Header.Set("x-amz-storage-class", "REDUCED_REDUNDANCY")
	checkVector(t, req, payload,
		strings.Join([]string{
			"PUT", "/test%24file.text", "",
			"date:Fri, 24 May 2013 00:00:00 GMT",
			"host:examplebucket.s3.amazonaws.com",
			"x-amz-content-sha256:" + payload,
			"x-amz-date:20130524T000000Z",
			"x-amz-storage-class:REDUCED_REDUNDANCY",
			"",
			"date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class",
			payload,
		}, "\n"),
		"AWS4-HMAC-SHA256\n20130524T000000Z\n20130524/us-east-1/s3/aws4_request\n9e0e90d9c76de8fa5b200d8c849cd5b8dc7a3be3951ddb7f6a76b4158342019d",
		"98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd",
		"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,"+
			"SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class,"+
			"Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd")
}

func TestSigV4_GetBucketLifecycle(t *testing.T) {
	req := newAWSReq(t, "GET", "https://examplebucket.s3.amazonaws.com/?lifecycle")
	checkVector(t, req, emptyPayload,
		strings.Join([]string{
			"GET", "/", "lifecycle=",
			"host:examplebucket.s3.amazonaws.com",
			"x-amz-content-sha256:" + emptyPayload,
			"x-amz-date:20130524T000000Z",
			"",
			"host;x-amz-content-sha256;x-amz-date",
			emptyPayload,
		}, "\n"),
		"AWS4-HMAC-SHA256\n20130524T000000Z\n20130524/us-east-1/s3/aws4_request\n9766c798316ff2757b517bc739a67f6213b4ab36dd5da2f94eaebf79c77395ca",
		"fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543",
		"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,"+
			"SignedHeaders=host;x-amz-content-sha256;x-amz-date,"+
			"Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543")
}

func TestSigV4_ListObjectsSortedQuery(t *testing.T) {
	req := newAWSReq(t, "GET", "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J")
	awsSigner.SignRequest(req, emptyPayload, awsTime)
	want := "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"
	if got := req.Header.Get("Authorization"); !strings.HasSuffix(got, "Signature="+want) {
		t.Errorf("list objects signature: got %s, want suffix %s", got, want)
	}
}

func TestSigV4_PresignGet(t *testing.T) {
	req := newAWSReq(t, "GET", "https://examplebucket.s3.amazonaws.com/test.txt")
	got := awsSigner.Presign(req, 86400*time.Second, awsTime)
	want := "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
		"&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host" +
		"&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Errorf("presigned URL mismatch\n got: %s\nwant: %s", got, want)
	}
	canonical, sts, _, _ := awsSigner.details(newAWSReq(t, "GET", "https://examplebucket.s3.amazonaws.com/test.txt"),
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host",
		unsignedPayload, awsTime)
	wantCanonical := "GET\n/test.txt\nX-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host\nhost:examplebucket.s3.amazonaws.com\n\nhost\nUNSIGNED-PAYLOAD"
	if canonical != wantCanonical {
		t.Errorf("presign canonical request mismatch\n got: %q\nwant: %q", canonical, wantCanonical)
	}
	if !strings.HasSuffix(sts, "3bfa292879f6447bbcda7001decf97f4a54dc650c8942174ae0a9121cf58ad04") {
		t.Errorf("presign string to sign: %q", sts)
	}
}

func TestURIEncode(t *testing.T) {
	cases := map[string]string{"a b/c": "a%20b%2Fc", "~-_.": "~-_.", "中": "%E4%B8%AD", "$&": "%24%26"}
	for in, want := range cases {
		if got := uriEncode(in, true); got != want {
			t.Errorf("uriEncode(%q)=%q want %q", in, got, want)
		}
	}
	if got := uriEncode("a b/c", false); got != "a%20b/c" {
		t.Errorf("slash kept: %q", got)
	}
}
