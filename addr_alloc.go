package main

//
// IP address allocation for VPNs, to ensure a globally unique IP address.
// A set of hard-coded values here e.g. addresses are IPv4 addresses in the
// range 10.8.0.2 .. 10.92.255.254.
//
// Requests are of the form: https://server/device-name
// Responses are plain text payloads with a human-readable IPv4 address.
// If a device has not been seen before, it is allocated a new address.
//

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	// Bolt is a simple key-value store.
	"encoding/json"
	"github.com/boltdb/bolt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

var (

	// First IP address to allocate.
	ini = net.ParseIP("10.8.0.2").To4()

	// Last IP address to allocate.  An attempt to allocate this address
	// will fail.
	fin = net.ParseIP("10.92.255.255").To4()
)

// State information.
type Handler struct {

	// Key-value store.
	db *bolt.DB

	// Next IP address to allocate.
	next net.IP
}

// From an IP address, calculate the 'next' one.
func nextIP(a net.IP) {

	a[3]++
	if a[3] == 0 {
		a[3] = 0
		a[2]++
		if a[2] == 0 {
			a[2] = 0
			a[1]++
			if a[1] == 0 {
				a[1] = 0
				a[0]++
			}
		}
	}

}

// HTTP request handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if r.URL.Path == "/all" {
		h.ServeAll(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/get/") {
		h.ServeGet(w, r, strings.TrimPrefix(r.URL.Path, "/get/"))
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, "Not found.")
	return

}

func (h *Handler) ServeAll(w http.ResponseWriter, r *http.Request) {
	// Find next available IP address.

	mappings := map[string]string{}

	h.db.Update(func(tx *bolt.Tx) error {

		// Create bucket
		b, err := tx.CreateBucketIfNotExists([]byte("addresses"))
		if err != nil {
			log.Fatal(err)
		}

		// Cursor on all keys.
		c := b.Cursor()

		// Loop through all keys.
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var ip net.IP = v
			mappings[string(k)] = ip.To4().String()
		}

		return nil
	})

	b, err := json.Marshal(mappings)
	if err != nil {
		log.Fatal("Couldn't marshal json: %s", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, string(b))
	return

}

func (h *Handler) ServeGet(w http.ResponseWriter, r *http.Request,
	device string) {

	var addr string
	found := false

	// See if this address is already in the database.
	err := h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("addresses"))
		v := b.Get([]byte(device))
		if v != nil {
			var v2 net.IP = v
			addr = v2.To4().String()
			fmt.Printf("Device %s: returning %s\n", device, addr)
			found = true
		}
		return nil
	})

	// Handle failure with a 500 status.
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "Database lookup failed.")
		return
	}

	// If not found...
	if !found {

		// If we've run out of addresses, that's a 500 error.
		if bytes.Compare(h.next, fin) == 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "Ran out of IP addresses.")
			return
		}

		// Allocate new address.
		addr = h.next.String()
		fmt.Printf("Device %s: allocating: %s\n", device, addr)

		// Write address to database.
		err = h.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("addresses"))
			if err != nil {
				return err
			}
			err = b.Put([]byte(device), h.next)
			if err != nil {
				return err
			}

			return nil

		})

		// Throw error if allocation failed.
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "Database write failed.")
			return
		}

		// Address is allocated, increment next address.
		nextIP(h.next)

	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, addr)
	return

}

func main() {

	// Get CA certs.
	caCert, err := ioutil.ReadFile("/key/cert.ca")
	if err != nil {
		log.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Create TLS configuration.  Client certificates are mandatory.
	tlsConfig := &tls.Config{
		ClientCAs:  caCertPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
	tlsConfig.BuildNameToCertificate()

	handler := &Handler{}

	// Open database.
	handler.db, err = bolt.Open("/addresses/addr.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}

	handler.next = ini

	// Find next available IP address.
	handler.db.Update(func(tx *bolt.Tx) error {

		// Create bucket
		b, err := tx.CreateBucketIfNotExists([]byte("addresses"))
		if err != nil {
			log.Fatal(err)
		}

		// Cursor on all keys.
		c := b.Cursor()

		// Loop through all keys.
		for k, v := c.First(); k != nil; k, v = c.Next() {

			var ip net.IP = v
			ip = ip.To4()

			fmt.Printf("Existing allocation: %s: %s\n",
				k, ip.String())

			// Look for a higher key than the last seen.
			if bytes.Compare(ip, handler.next) >= 0 {
				handler.next = net.IPv4(ip[0], ip[1], ip[2],
					ip[3]).To4()

				// Increment highest key to make next available
				// free.
				nextIP(handler.next)
			}

		}

		return nil
	})

	fmt.Printf("Next free address is %s\n", handler.next.String())

	// Start HTTPS server.
	s := &http.Server{
		Addr:           ":443",
		Handler:        handler,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
		TLSConfig:      tlsConfig,
	}
	log.Fatal(s.ListenAndServeTLS("/key/cert.allocator",
		"/key/key.allocator"))

}
