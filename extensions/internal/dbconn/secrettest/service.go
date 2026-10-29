package secrettest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// The paths and names of the fake service.
const (
	serviceName    = "org.freedesktop.secrets"
	servicePath    = dbus.ObjectPath("/org/freedesktop/secrets")
	collectionPath = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	noObject       = dbus.ObjectPath("/")
)

// oakleyGroup2 is the prime of the second Oakley group (RFC 2409): the fake has its own copy so that a typo in the client's is
// found by a test and not copied here.
const oakleyGroup2 = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74" +
	"020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F1437" +
	"4FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
	"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF"

// Options configure the fake service.
type Options struct {
	// Locked starts the collection locked: a client has to unlock it through a prompt.
	Locked bool
	// Dismiss makes the user dismiss every prompt.
	Dismiss bool
	// NoEncryption makes the service refuse the key exchange and offer only the plain algorithm.
	NoEncryption bool
	// NoCollection makes ReadAlias("default") answer "/", as a desktop with no default keyring does.
	NoCollection bool
	// NeverAnswerPrompt makes a prompt that never completes (the user walked away).
	NeverAnswerPrompt bool
}

// Item is a secret as the service keeps it, for the assertions of a test.
type Item struct {
	Label      string
	Attributes map[string]string
	Secret     string
}

// Service is a fake Secret Service on a private bus.
type Service struct {
	t    testing.TB
	conn *dbus.Conn
	opts Options

	mu       sync.Mutex
	locked   bool
	items    map[dbus.ObjectPath]*item
	sessions map[dbus.ObjectPath][]byte // the AES key of an encrypted session, nil for a plain one
	prompts  map[dbus.ObjectPath]func(dismissed bool) dbus.Variant
	calls    []string
	next     int
	plainOn  bool
}

type item struct {
	label  string
	attrs  map[string]string
	secret []byte
}

// StartService puts a fake Secret Service on the bus. It stops with the test.
func StartService(t testing.TB, b *Bus, opts Options) *Service {
	t.Helper()
	conn, err := b.Dial(t.Context())
	if err != nil {
		t.Fatalf("connecting the fake service to the bus: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("closing the connection of the fake service: %v", err)
		}
	})
	s := &Service{t: t, conn: conn, opts: opts, locked: opts.Locked, items: map[dbus.ObjectPath]*item{},
		sessions: map[dbus.ObjectPath][]byte{}, prompts: map[dbus.ObjectPath]func(bool) dbus.Variant{}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("exporting the fake service: %v", err)
		}
	}
	must(conn.Export(svcObject{s}, servicePath, "org.freedesktop.Secret.Service"))
	must(conn.Export(collObject{s}, collectionPath, "org.freedesktop.Secret.Collection"))
	must(conn.Export(collProps{s}, collectionPath, "org.freedesktop.DBus.Properties"))
	reply, err := conn.RequestName(serviceName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("owning %s: %v (%v)", serviceName, err, reply)
	}
	return s
}

// Items returns the secrets the service holds, in the order of their paths.
func (s *Service) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := make([]string, 0, len(s.items))
	for p := range s.items {
		paths = append(paths, string(p))
	}
	sort.Strings(paths)
	var out []Item
	for _, p := range paths {
		it := s.items[dbus.ObjectPath(p)]
		attrs := map[string]string{}
		for k, v := range it.attrs {
			attrs[k] = v
		}
		out = append(out, Item{Label: it.label, Attributes: attrs, Secret: string(it.secret)})
	}
	return out
}

// Calls returns the methods the clients called, in order ("OpenSession(dh-ietf1024-sha256-aes128-cbc-pkcs7)", "Unlock", ...).
func (s *Service) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// Locked tells whether the collection is locked.
func (s *Service) Locked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locked
}

// SawPlainOnTheWire tells whether a client sent or received a secret through a plain session.
func (s *Service) SawPlainOnTheWire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plainOn
}

// Put stores a secret directly, as another application would have.
func (s *Service) Put(label string, attrs map[string]string, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := dbus.ObjectPath(fmt.Sprintf("%s/%d", collectionPath, s.bump()))
	s.items[path] = &item{label: label, attrs: attrs, secret: []byte(secret)}
	s.exportItem(path)
}

func (s *Service) bump() int { s.next++; return s.next }

func (s *Service) record(call string) {
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
}

// fail is the error of a method of the fake: org.freedesktop.DBus.Error.Failed with a message.
func fail(format string, a ...any) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.Failed", []any{fmt.Sprintf(format, a...)})
}

// exportItem puts the object of an item on the bus. The caller holds the lock.
func (s *Service) exportItem(path dbus.ObjectPath) {
	if err := s.conn.Export(itemObject{s, path}, path, "org.freedesktop.Secret.Item"); err != nil {
		s.t.Errorf("exporting an item: %v", err)
	}
	if err := s.conn.Export(itemProps{s}, path, "org.freedesktop.DBus.Properties"); err != nil {
		s.t.Errorf("exporting the properties of an item: %v", err)
	}
}

// newPrompt makes a prompt object whose completion runs done. The caller holds the lock.
func (s *Service) newPrompt(done func(dismissed bool) dbus.Variant) dbus.ObjectPath {
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/secrets/prompt/p%d", s.bump()))
	s.prompts[path] = done
	if err := s.conn.Export(promptObject{s, path}, path, "org.freedesktop.Secret.Prompt"); err != nil {
		s.t.Errorf("exporting a prompt: %v", err)
	}
	return path
}

// ---- the service ----

type svcObject struct{ s *Service }

type secretStruct struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// OpenSession implements org.freedesktop.Secret.Service.OpenSession.
func (o svcObject) OpenSession(algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	s := o.s
	s.record("OpenSession(" + algorithm + ")")
	s.mu.Lock()
	defer s.mu.Unlock()
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/secrets/session/s%d", s.bump()))
	switch algorithm {
	case "plain":
		s.sessions[path] = nil
		s.plainOn = true
	case "dh-ietf1024-sha256-aes128-cbc-pkcs7":
		if s.opts.NoEncryption {
			return dbus.Variant{}, "", dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"no key exchange"})
		}
		clientPub, ok := input.Value().([]byte)
		if !ok {
			return dbus.Variant{}, "", fail("the input of the key exchange is not bytes")
		}
		p, _ := new(big.Int).SetString(oakleyGroup2, 16)
		x, err := rand.Int(rand.Reader, new(big.Int).Sub(p, big.NewInt(3)))
		if err != nil {
			return dbus.Variant{}, "", fail("%v", err)
		}
		x.Add(x, big.NewInt(2))
		pub := new(big.Int).Exp(big.NewInt(2), x, p).FillBytes(make([]byte, 128))
		shared := new(big.Int).Exp(new(big.Int).SetBytes(clientPub), x, p).FillBytes(make([]byte, 128))
		key, err := hkdf.Key(sha256.New, shared, nil, "", 16)
		if err != nil {
			return dbus.Variant{}, "", fail("%v", err)
		}
		s.sessions[path] = key
		if err := s.conn.Export(sessionObject{s, path}, path, "org.freedesktop.Secret.Session"); err != nil {
			return dbus.Variant{}, "", fail("%v", err)
		}
		return dbus.MakeVariant(pub), path, nil
	default:
		return dbus.Variant{}, "", dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{algorithm})
	}
	if err := s.conn.Export(sessionObject{s, path}, path, "org.freedesktop.Secret.Session"); err != nil {
		return dbus.Variant{}, "", fail("%v", err)
	}
	return dbus.MakeVariant(""), path, nil
}

// ReadAlias implements org.freedesktop.Secret.Service.ReadAlias.
func (o svcObject) ReadAlias(name string) (dbus.ObjectPath, *dbus.Error) {
	o.s.record("ReadAlias(" + name + ")")
	if name != "default" || o.s.opts.NoCollection {
		return noObject, nil
	}
	return collectionPath, nil
}

// Unlock implements org.freedesktop.Secret.Service.Unlock: a locked collection needs the user, so the answer is a prompt.
func (o svcObject) Unlock(objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	s := o.s
	s.record("Unlock")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locked {
		return objects, noObject, nil
	}
	prompt := s.newPrompt(func(dismissed bool) dbus.Variant {
		if !dismissed {
			s.locked = false
		}
		return dbus.MakeVariant([]dbus.ObjectPath{})
	})
	return []dbus.ObjectPath{}, prompt, nil
}

// SearchItems implements org.freedesktop.Secret.Service.SearchItems.
func (o svcObject) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	s := o.s
	s.record("SearchItems")
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []dbus.ObjectPath
	var paths []string
	for p := range s.items {
		paths = append(paths, string(p))
	}
	sort.Strings(paths)
	for _, p := range paths {
		it := s.items[dbus.ObjectPath(p)]
		match := true
		for k, v := range attrs {
			if it.attrs[k] != v {
				match = false
			}
		}
		if match {
			found = append(found, dbus.ObjectPath(p))
		}
	}
	if s.locked {
		return []dbus.ObjectPath{}, found, nil
	}
	return found, []dbus.ObjectPath{}, nil
}

// ---- sessions, collection, items, prompts ----

type sessionObject struct {
	s    *Service
	path dbus.ObjectPath
}

// Close implements org.freedesktop.Secret.Session.Close.
func (o sessionObject) Close() *dbus.Error {
	o.s.record("Session.Close")
	o.s.mu.Lock()
	delete(o.s.sessions, o.path)
	o.s.mu.Unlock()
	return nil
}

type collObject struct{ s *Service }

// CreateItem implements org.freedesktop.Secret.Collection.CreateItem.
func (o collObject) CreateItem(props map[string]dbus.Variant, sec secretStruct, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	s := o.s
	s.record("CreateItem")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return noObject, noObject, dbus.NewError("org.freedesktop.Secret.Error.IsLocked", []any{"the collection is locked"})
	}
	key, ok := s.sessions[sec.Session]
	if !ok {
		return noObject, noObject, dbus.NewError("org.freedesktop.Secret.Error.NoSession", nil)
	}
	value := sec.Value
	if key != nil {
		plain, reason := decrypt(key, sec.Parameters, sec.Value)
		if reason != "" {
			return noObject, noObject, fail("%s", reason)
		}
		value = plain
	} else {
		s.plainOn = true
	}
	label, _ := props["org.freedesktop.Secret.Item.Label"].Value().(string)
	attrs, _ := props["org.freedesktop.Secret.Item.Attributes"].Value().(map[string]string)
	if replace {
		for p, it := range s.items {
			same := len(it.attrs) == len(attrs)
			for k, v := range attrs {
				if it.attrs[k] != v {
					same = false
				}
			}
			if same {
				delete(s.items, p)
				if err := s.conn.Export(nil, p, "org.freedesktop.Secret.Item"); err != nil {
					s.t.Errorf("unexporting an item: %v", err)
				}
			}
		}
	}
	path := dbus.ObjectPath(fmt.Sprintf("%s/%d", collectionPath, s.bump()))
	s.items[path] = &item{label: label, attrs: attrs, secret: value}
	s.exportItem(path)
	return path, noObject, nil
}

type collProps struct{ s *Service }

// Get implements org.freedesktop.DBus.Properties.Get for the collection.
func (o collProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	o.s.record("Collection.Get(" + name + ")")
	if name == "Locked" {
		return dbus.MakeVariant(o.s.Locked()), nil
	}
	return dbus.Variant{}, fail("no property %s", name)
}

type itemObject struct {
	s    *Service
	path dbus.ObjectPath
}

// GetSecret implements org.freedesktop.Secret.Item.GetSecret.
func (o itemObject) GetSecret(session dbus.ObjectPath) (secretStruct, *dbus.Error) {
	s := o.s
	s.record("GetSecret")
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[o.path]
	if !ok {
		return secretStruct{}, dbus.NewError("org.freedesktop.Secret.Error.NoSuchObject", nil)
	}
	if s.locked {
		return secretStruct{}, dbus.NewError("org.freedesktop.Secret.Error.IsLocked", nil)
	}
	key, ok := s.sessions[session]
	if !ok {
		return secretStruct{}, dbus.NewError("org.freedesktop.Secret.Error.NoSession", nil)
	}
	if key == nil {
		s.plainOn = true
		return secretStruct{Session: session, Parameters: []byte{}, Value: it.secret, ContentType: "text/plain"}, nil
	}
	iv, ct, reason := encrypt(key, it.secret)
	if reason != "" {
		return secretStruct{}, fail("%s", reason)
	}
	return secretStruct{Session: session, Parameters: iv, Value: ct, ContentType: "text/plain"}, nil
}

// Delete implements org.freedesktop.Secret.Item.Delete.
func (o itemObject) Delete() (dbus.ObjectPath, *dbus.Error) {
	s := o.s
	s.record("Delete")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return noObject, dbus.NewError("org.freedesktop.Secret.Error.IsLocked", nil)
	}
	delete(s.items, o.path)
	return noObject, nil
}

type itemProps struct{ s *Service }

// Get implements org.freedesktop.DBus.Properties.Get for an item.
func (o itemProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if name == "Locked" {
		return dbus.MakeVariant(o.s.Locked()), nil
	}
	return dbus.Variant{}, fail("no property %s", name)
}

type promptObject struct {
	s    *Service
	path dbus.ObjectPath
}

// Prompt implements org.freedesktop.Secret.Prompt.Prompt: it completes later, with the answer of the user the options describe.
func (o promptObject) Prompt(window string) *dbus.Error {
	s := o.s
	s.record("Prompt")
	s.mu.Lock()
	done := s.prompts[o.path]
	dismiss := s.opts.Dismiss
	never := s.opts.NeverAnswerPrompt
	s.mu.Unlock()
	if never {
		return nil
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.mu.Lock()
		result := done(dismiss)
		s.mu.Unlock()
		if err := s.conn.Emit(o.path, "org.freedesktop.Secret.Prompt.Completed", dismiss, result); err != nil {
			s.t.Logf("emitting Completed: %v", err)
		}
	}()
	return nil
}

// Dismiss implements org.freedesktop.Secret.Prompt.Dismiss.
func (o promptObject) Dismiss() *dbus.Error {
	o.s.record("Prompt.Dismiss")
	return nil
}

// ---- the cipher ----

// encrypt encrypts a secret with AES-128-CBC and PKCS#7 padding under a random IV; reason says why it could not.
func encrypt(key, secret []byte) (iv, ct []byte, reason string) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err.Error()
	}
	iv = make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, err.Error()
	}
	pad := aes.BlockSize - len(secret)%aes.BlockSize
	padded := append([]byte(nil), secret...)
	for range pad {
		padded = append(padded, byte(pad))
	}
	ct = make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	return iv, ct, ""
}

// decrypt undoes encrypt; reason says why it could not.
func decrypt(key, iv, ct []byte) (plain []byte, reason string) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err.Error()
	}
	if len(iv) != aes.BlockSize || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Sprintf("bad secret: iv %d, length %d", len(iv), len(ct))
	}
	plain = make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
	pad := int(plain[len(plain)-1])
	if pad == 0 || pad > aes.BlockSize {
		return nil, "bad padding " + strconv.Itoa(pad)
	}
	return plain[:len(plain)-pad], ""
}
