package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
)

// buildProxyHandler configures and returns the proxy handler.
func buildProxyHandler(mitmEnabled bool, store *captureStore, broker *sseBroker, caDur string) http.Handler {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: true,
	}
	proxy.Tr = tr

	// Enable MITM if requested
	if mitmEnabled {
		if err := enableMITM(proxy, true, caDur); err != nil {
			log.Fatalf("MITM init failed: %v", err)
		}
	} else {
		log.Println("MITM disabled: HTTPS will be tunneled (opaque bodies).")
	}

	// Ephemeral map for partial captures
	var reqMap sync.Map

	// Capture request
	proxy.OnRequest().DoFunc(func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		log.Printf("Proxy Request: %s %s", r.Method, r.URL.String())
		if isPaused() {
			// Do not record; just pass through unchanged.
			return r, nil
		}
		start := time.Now()
		p := &phases{startRT: start}
		key := reqKey(r)
		phaseMap.Store(key, p)

		ct := &httptrace.ClientTrace{
			DNSStart:          func(httptrace.DNSStartInfo) { p.dnsStart = time.Now() },
			DNSDone:           func(httptrace.DNSDoneInfo) { p.dnsEnd = time.Now() },
			ConnectStart:      func(_, _ string) { p.conStart = time.Now() },
			ConnectDone:       func(_, _ string, _ error) { p.conEnd = time.Now() },
			TLSHandshakeStart: func() { p.tlsStart = time.Now() },
			TLSHandshakeDone: func(cs tls.ConnectionState, _ error) {
				p.tlsEnd = time.Now()
				p.h2 = (cs.NegotiatedProtocol == "h2")
			},
			GotConn: func(ci httptrace.GotConnInfo) {
				p.reused = ci.Reused
				if ci.Conn != nil && ci.Conn.RemoteAddr() != nil {
					p.serverAddr = ci.Conn.RemoteAddr().String()
				}
			},
			WroteRequest:         func(httptrace.WroteRequestInfo) { p.wroteReq = time.Now() },
			GotFirstResponseByte: func() { p.firstByte = time.Now() },
		}

		reqHeaders := make(map[string][]string, len(r.Header))
		for k, v := range r.Header {
			reqHeaders[k] = append([]string(nil), v...)
		}
		encoding := r.Header.Get("Content-Encoding")
		bodyStr, newBody, err := readLimitedBody(r.Body, maxStoredBody, encoding)
		if err != nil {
			log.Printf("error reading request body: %v", err)
			bodyStr = "--body-read-error--"
			newBody = io.NopCloser(bytes.NewReader(nil))
		}
		r.Body = newBody

		c := Capture{
			Time:              time.Now().UTC(),
			Method:            r.Method,
			URL:               r.URL.String(),
			RequestHeaders:    reqHeaders,
			RequestBodyBase64: bodyStr,
			Notes:             fmt.Sprintf("pending (captured at %s)", start.Format(time.RFC3339)),
		}

		ctx.UserData = start
		reqMap.Store(key, c)
		r = r.WithContext(httptrace.WithClientTrace(r.Context(), ct))
		return r, nil
	})

	// Capture response
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if isPaused() || resp == nil || ctx == nil || ctx.Req == nil {
			return resp
		}
		key := reqKey(ctx.Req)
		val, ok := reqMap.Load(key)
		if !ok {
			return resp
		}
		partial := val.(Capture)

		if v, ok := phaseMap.Load(key); ok {
			p := v.(*phases)
			if p.done.IsZero() {
				p.done = time.Now()
			}

			partial.DNSMs = millis(p.dnsStart, p.dnsEnd)
			partial.ConnectMs = millis(p.conStart, p.conEnd)
			partial.TLSMs = millis(p.tlsStart, p.tlsEnd)
			partial.TTFBMs = millis(p.wroteReq, p.firstByte)
			partial.RespReadMs = millis(p.firstByte, p.done)
			partial.TotalMs = millis(p.startRT, p.done)
			partial.ServerAddr = p.serverAddr
			partial.ReusedConn = p.reused
			if resp.Request != nil && resp.Request.TLS != nil {
				partial.HTTP2 = (resp.Request.TLS.NegotiatedProtocol == "h2")
			} else {
				partial.HTTP2 = p.h2
			}
			phaseMap.Delete(key)
		}

		encoding := resp.Header.Get("Content-Encoding")
		respBodyStr, newRespBody, err := readLimitedBody(resp.Body, maxStoredBody, strings.ToLower(encoding))
		if err != nil {
			respBodyStr = "--resp-body-read-error--"
			newRespBody = io.NopCloser(bytes.NewReader(nil))
		}
		resp.Body = newRespBody

		rh := make(map[string][]string, len(resp.Header))
		for k, v := range resp.Header {
			rh[k] = append([]string(nil), v...)
		}

		var durationMs int64
		if st, ok := ctx.UserData.(time.Time); ok {
			durationMs = time.Since(st).Milliseconds()
		}

		if partial.Name == "" {
			partial.Name = fmt.Sprintf("%s %s [%d]", partial.Method, partial.URL, resp.StatusCode)
		}
		partial.ResponseStatus = resp.StatusCode
		partial.ResponseHeaders = rh
		partial.ResponseBodyBase64 = respBodyStr
		partial.DurationMs = durationMs
		partial.Notes = ""

		stored := store.add(partial)
		broker.publish(stored)
		reqMap.Delete(key)

		log.Printf("Response '%s' Status %s", resp.Request.URL.String(), resp.Status)
		return resp
	})

	return proxy
}

// reqKey returns a stable string key for a request pointer
func reqKey(r *http.Request) string {
	return fmt.Sprintf("%p", r)
}

// small helper: read up to N bytes and return as string (base64 would be safer for arbitrary bytes).
// imports needed:
//   "bytes"
//   "compress/gzip"
//   "compress/zlib"
//   "io"
//   "io/ioutil"
//   "strings"

func readLimitedBody(rc io.ReadCloser, max int, encoding string) (string, io.ReadCloser, error) {
	if rc == nil {
		return "", ioutil.NopCloser(bytes.NewReader(nil)), nil
	}
	defer rc.Close()

	var buf bytes.Buffer
	limited := io.LimitReader(rc, int64(max)+1) // read up to max+1 to detect truncation
	n, err := io.Copy(&buf, limited)
	if err != nil {
		return "", ioutil.NopCloser(bytes.NewReader(nil)), err
	}
	raw := buf.Bytes()

	// Always return the ORIGINAL bytes to the caller for reconstituting r.Body,
	// so proxying behavior is unchanged.
	restore := ioutil.NopCloser(bytes.NewReader(raw))

	// If we exceeded the cap, we cannot reliably decompress; return truncated marker.
	if n > int64(max) {
		return string(raw[:max]) + "\n--truncated--", restore, nil
	}

	// Try to decode per Content-Encoding for DISPLAY ONLY.
	// We keep the original compressed bytes in the returned ReadCloser.
	enc := strings.ToLower(strings.TrimSpace(encoding))
	switch enc {
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err == nil {
			defer gr.Close()
			if dec, derr := ioutil.ReadAll(gr); derr == nil {
				// Trim to max if decompressed payload is too large (should be rare here since n<=max).
				if len(dec) > max {
					return string(dec[:max]) + "\n--truncated--", restore, nil
				}
				return string(dec), restore, nil
			}
		}
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			defer zr.Close()
			if dec, derr := ioutil.ReadAll(zr); derr == nil {
				if len(dec) > max {
					return string(dec[:max]) + "\n--truncated--", restore, nil
				}
				return string(dec), restore, nil
			}
		}
	}

	// Fallback: treat as plain text
	return string(raw), restore, nil
}

// generateCA returns both:
// - parsedCert: *x509.Certificate  (suitable for goproxy.GoproxyCa)
// - parsedKey:  *rsa.PrivateKey    (suitable for goproxy.GoproxyCaKey)
// - certPEM, keyPEM []byte         (PEM bytes, useful to build tls.Certificate)
func generateCA() (parsedCert *x509.Certificate, parsedKey *rsa.PrivateKey, certPEM, keyPEM []byte, err error) {
	// generate key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// create certificate template
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Example MITM CA"},
			CommonName:   "Example MITM CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	parsed, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// pem encode cert and key
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return parsed, priv, certPEM, keyPEM, nil
}

// call this before starting the proxy server
func enableMITM(proxy *goproxy.ProxyHttpServer, persist bool, dir string) error {
	// Load or generate an X.509 CA (your existing helpers are fine)
	var (
		caX509 *x509.Certificate
		caKey  *rsa.PrivateKey
		err    error
	)
	if persist {
		caX509, caKey, err = loadOrCreateCA(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca.key"))
	} else {
		caX509, caKey, err = createEphemeralCA()
	}
	if err != nil {
		return err
	}

	// Build a tls.Certificate (this is what the current API expects)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caX509.Raw})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})

	caPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to build tls cert pair: %w", err)
	}

	// Create a TLS config generator from the CA pair
	tlsFromCA := goproxy.TLSConfigFromCA(&caPair) // returns func(host, ctx) (*tls.Config, error)

	// Instruct goproxy to MITM CONNECT using our CA
	proxy.OnRequest().HandleConnect(goproxy.FuncHttpsHandler(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return &goproxy.ConnectAction{
				Action:    goproxy.ConnectMitm,
				TLSConfig: tlsFromCA,
			}, host
		},
	))

	// Upstream transport: we usually skip verification since we’re intercepting
	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return nil
}

func loadOrCreateCA(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	// Try to load
	if cert, key, err := loadCA(certPath, keyPath); err == nil {
		return cert, key, nil
	}
	// Create and persist
	cert, key, err := createEphemeralCA()
	if err != nil {
		return nil, nil, err
	}
	if err := saveCA(cert, key, certPath, keyPath); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func loadCA(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	cb, _ := pem.Decode(certPEM)
	if cb == nil || cb.Type != "CERTIFICATE" {
		return nil, nil, errors.New("invalid CA cert PEM")
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil || (kb.Type != "RSA PRIVATE KEY" && kb.Type != "PRIVATE KEY") {
		return nil, nil, errors.New("invalid CA key PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	var key *rsa.PrivateKey
	if kb.Type == "RSA PRIVATE KEY" {
		key, err = x509.ParsePKCS1PrivateKey(kb.Bytes)
	} else {
		// If you later switch to PKCS8, add x509.ParsePKCS8PrivateKey here.
		return nil, nil, errors.New("unsupported private key format")
	}
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func saveCA(cert *x509.Certificate, key *rsa.PrivateKey, certPath, keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certOut, fs.FileMode(0o644)); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, keyOut, fs.FileMode(0o600)); err != nil {
		return err
	}
	return nil
}

func createEphemeralCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Go MITM Proxy CA"},
			CommonName:   "Go MITM Proxy CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            2,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// helper: decompress if gzip or deflate
func maybeDecompress(body []byte, encoding string) []byte {
	switch encoding {
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body
		}
		defer r.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			return body
		}
		return out
	case "deflate":
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return body
		}
		defer r.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			return body
		}
		return out
	default:
		return body
	}
}