// Copyright (C) 2013-2018 by Maxim Bublis <b@codemonkey.ru>
//
// Permission is hereby granted, free of charge, to any person obtaining
// a copy of this software and associated documentation files (the
// "Software"), to deal in the Software without restriction, including
// without limitation the rights to use, copy, modify, merge, publish,
// distribute, sublicense, and/or sell copies of the Software, and to
// permit persons to whom the Software is furnished to do so, subject to
// the following conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
// MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
// LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
// OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
// WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package uuid

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"net"
	"sync"
	"time"
)

// Difference in 100-nanosecond intervals between
// UUID epoch (October 15, 1582) and Unix epoch (January 1, 1970).
const epochStart = 122192928000000000

// EpochFunc is the function type used to provide the current time.
type EpochFunc func() time.Time

// HWAddrFunc is the function type used to provide hardware (MAC) addresses.
type HWAddrFunc func() (net.HardwareAddr, error)

// DefaultGenerator is the default UUID Generator used by this package.
var DefaultGenerator Generator = NewGen()

// NewV1 returns a UUID based on the current timestamp and MAC address.
func NewV1() (UUID, error) {
	return DefaultGenerator.NewV1()
}

// NewV1 returns a UUID based on the provided timestamp and MAC address.
func NewV1AtTime(atTime time.Time) (UUID, error) {
	return DefaultGenerator.NewV1AtTime(atTime)
}

// NewV3 returns a UUID based on the MD5 hash of the namespace UUID and name.
func NewV3(ns UUID, name string) UUID {
	return DefaultGenerator.NewV3(ns, name)
}

// NewV4 returns a randomly generated UUID.
func NewV4() (UUID, error) {
	return DefaultGenerator.NewV4()
}

// NewV5 returns a UUID based on SHA-1 hash of the namespace UUID and name.
func NewV5(ns UUID, name string) UUID {
	return DefaultGenerator.NewV5(ns, name)
}

// NewV6 returns a k-sortable UUID based on the current timestamp and 48 bits of
// pseudorandom data. The timestamp in a V6 UUID is the same as V1, with the bit
// order being adjusted to allow the UUID to be k-sortable.
func NewV6() (UUID, error) {
	return DefaultGenerator.NewV6()
}

// NewV6 returns a k-sortable UUID based on the provided timestamp and 48 bits of
// pseudorandom data. The timestamp in a V6 UUID is the same as V1, with the bit
// order being adjusted to allow the UUID to be k-sortable.
func NewV6AtTime(atTime time.Time) (UUID, error) {
	return DefaultGenerator.NewV6AtTime(atTime)
}

// NewV7 returns a k-sortable UUID based on the current millisecond-precision
// UNIX epoch and 74 bits of pseudorandom data. It supports single-node batch
// generation (multiple UUIDs in the same timestamp) with a Monotonic Random counter.
func NewV7() (UUID, error) {
	return DefaultGenerator.NewV7()
}

// NewV7 returns a k-sortable UUID based on the provided millisecond-precision
// UNIX epoch and 74 bits of pseudorandom data. It supports single-node batch
// generation (multiple UUIDs in the same timestamp) with a Monotonic Random counter.
func NewV7AtTime(atTime time.Time) (UUID, error) {
	return DefaultGenerator.NewV7AtTime(atTime)
}

// Generator provides an interface for generating UUIDs.
type Generator interface {
	NewV1() (UUID, error)
	NewV1AtTime(time.Time) (UUID, error)
	NewV3(ns UUID, name string) UUID
	NewV4() (UUID, error)
	NewV5(ns UUID, name string) UUID
	NewV6() (UUID, error)
	NewV6AtTime(time.Time) (UUID, error)
	NewV7() (UUID, error)
	NewV7AtTime(time.Time) (UUID, error)
}

// Gen is a reference UUID generator based on the specifications laid out in
// RFC-9562 and DCE 1.1: Authentication and Security Services. This type
// satisfies the Generator interface as defined in this package.
//
// For consumers who are generating V1 UUIDs, but don't want to expose the MAC
// address of the node generating the UUIDs, the NewGenWithHWAF() function has been
// provided as a convenience. See the function's documentation for more info.
//
// The authors of this package do not feel that the majority of users will need
// to obfuscate their MAC address, and so we recommend using NewGen() to create
// a new generator.
type Gen struct {
	clockSequenceOnce sync.Once
	hardwareAddrOnce  sync.Once
	storageMutex      sync.Mutex

	rand io.Reader

	epochFunc     EpochFunc
	hwAddrFunc    HWAddrFunc
	lastTime      uint64
	clockSequence uint16
	hardwareAddr  [6]byte
}

// GenOption is a function type that can be used to configure a Gen generator.
type GenOption func(*Gen)

// interface check -- build will fail if *Gen doesn't satisfy Generator
var _ Generator = (*Gen)(nil)

// NewGen returns a new instance of Gen with some default values set. Most
// people should use this.
func NewGen() *Gen {
	return NewGenWithHWAF(defaultHWAddrFunc)
}

// NewGenWithHWAF builds a new UUID generator with the HWAddrFunc provided. Most
// consumers should use NewGen() instead.
//
// This is used so that consumers can generate their own MAC addresses, for use
// in the generated UUIDs, if there is some concern about exposing the physical
// address of the machine generating the UUID.
//
// The Gen generator will only invoke the HWAddrFunc once, and cache that MAC
// address for all the future UUIDs generated by it. If you'd like to switch the
// MAC address being used, you'll need to create a new generator using this
// function.
func NewGenWithHWAF(hwaf HWAddrFunc) *Gen {
	return NewGenWithOptions(WithHWAddrFunc(hwaf))
}

// NewGenWithOptions returns a new instance of Gen with the options provided.
// Most people should use NewGen() or NewGenWithHWAF() instead.
//
// To customize the generator, you can pass in one or more GenOption functions.
// For example:
//
//	gen := NewGenWithOptions(
//	    WithHWAddrFunc(myHWAddrFunc),
//	    WithEpochFunc(myEpochFunc),
//	    WithRandomReader(myRandomReader),
//	)
//
// NewGenWithOptions(WithHWAddrFunc(myHWAddrFunc)) is equivalent to calling
// NewGenWithHWAF(myHWAddrFunc)
// NewGenWithOptions() is equivalent to calling NewGen()
func NewGenWithOptions(opts ...GenOption) *Gen {
	gen := &Gen{
		epochFunc:  time.Now,
		hwAddrFunc: defaultHWAddrFunc,
		rand:       rand.Reader,
	}

	for _, opt := range opts {
		opt(gen)
	}

	return gen
}

// MonotonicGen extends the Gen struct with a counter for batch generation.
//
// MonotonicGen ensures the generation of strictly monotonic UUIDs within a
// batch by utilizing a counter in conjunction with timestamps. This is
// particularly useful for applications requiring ordered identifiers, such
// as database indices or log sequencing.
type MonotonicGen struct {
	Gen
	monotonicCounter uint16
	monotonicMutex   sync.Mutex
}

// NewMonotonicGen creates a MonotonicGen instance with configurable options.
//
// Arguments:
// - opts: Configuration options for the generator.
//
// Returns:
// - *MonotonicGen: The configured generator.
func NewMonotonicGen(opts ...GenOption) *MonotonicGen {
	gen := &MonotonicGen{
		Gen: *NewGenWithOptions(opts...),
	}
	return gen
}

// WithHWAddrFunc is a GenOption that allows you to provide your own HWAddrFunc
// function.
// When this option is nil, the defaultHWAddrFunc is used.
func WithHWAddrFunc(hwaf HWAddrFunc) GenOption {
	return func(gen *Gen) {
		if hwaf == nil {
			hwaf = defaultHWAddrFunc
		}

		gen.hwAddrFunc = hwaf
	}
}

// WithEpochFunc is a GenOption that allows you to provide your own EpochFunc
// function.
// When this option is nil, time.Now is used.
func WithEpochFunc(epochf EpochFunc) GenOption {
	return func(gen *Gen) {
		if epochf == nil {
			epochf = time.Now
		}

		gen.epochFunc = epochf
	}
}

// WithRandomReader is a GenOption that allows you to provide your own random
// reader.
// When this option is nil, the default rand.Reader is used.
func WithRandomReader(reader io.Reader) GenOption {
	return func(gen *Gen) {
		if reader == nil {
			reader = rand.Reader
		}

		gen.rand = reader
	}
}

// NewV1 returns a UUID based on the current timestamp and MAC address.
func (g *Gen) NewV1() (UUID, error) {
	return g.NewV1AtTime(g.epochFunc())
}

// NewV1AtTime returns a UUID based on the provided timestamp and current MAC address.
func (g *Gen) NewV1AtTime(atTime time.Time) (UUID, error) {
	u := UUID{}

	timeNow, clockSeq, err := g.getClockSequence(false, atTime)
	if err != nil {
		return Nil, err
	}
	binary.BigEndian.PutUint32(u[0:], uint32(timeNow))
	binary.BigEndian.PutUint16(u[4:], uint16(timeNow>>32))
	binary.BigEndian.PutUint16(u[6:], uint16(timeNow>>48))
	binary.BigEndian.PutUint16(u[8:], clockSeq)

	hardwareAddr, err := g.getHardwareAddr()
	if err != nil {
		return Nil, err
	}
	copy(u[10:], hardwareAddr)

	u.SetVersion(V1)
	u.SetVariant(VariantRFC9562)

	return u, nil
}

// NewV3 returns a UUID based on the MD5 hash of the namespace UUID and name.
func (g *Gen) NewV3(ns UUID, name string) UUID {
	u := newFromHash(md5.New(), ns, name)
	u.SetVersion(V3)
	u.SetVariant(VariantRFC9562)

	return u
}

// NewV4 returns a randomly generated UUID.
func (g *Gen) NewV4() (UUID, error) {
	u := UUID{}
	if _, err := io.ReadFull(g.rand, u[:]); err != nil {
		return Nil, err
	}
	u.SetVersion(V4)
	u.SetVariant(VariantRFC9562)

	return u, nil
}

// NewV5 returns a UUID based on SHA-1 hash of the namespace UUID and name.
func (g *Gen) NewV5(ns UUID, name string) UUID {
	u := newFromHash(sha1.New(), ns, name)
	u.SetVersion(V5)
	u.SetVariant(VariantRFC9562)

	return u
}

// NewV6 returns a k-sortable UUID based on the current timestamp and 48 bits of
// pseudorandom data. The timestamp in a V6 UUID is the same as V1, with the bit
// order being adjusted to allow the UUID to be k-sortable.
func (g *Gen) NewV6() (UUID, error) {
	return g.NewV6AtTime(g.epochFunc())
}

// NewV6 returns a k-sortable UUID based on the provided timestamp and 48 bits of
// pseudorandom data. The timestamp in a V6 UUID is the same as V1, with the bit
// order being adjusted to allow the UUID to be k-sortable.
func (g *Gen) NewV6AtTime(atTime time.Time) (UUID, error) {
	/* https://datatracker.ietf.org/doc/html/rfc9562#name-uuid-version-6
	    0                   1                   2                   3
	    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |                           time_high                           |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |           time_mid            |  ver  |       time_low        |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |var|         clock_seq         |             node              |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |                              node                             |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ */
	var u UUID

	timeNow, _, err := g.getClockSequence(false, atTime)
	if err != nil {
		return Nil, err
	}

	binary.BigEndian.PutUint32(u[0:], uint32(timeNow>>28))   // set time_high
	binary.BigEndian.PutUint16(u[4:], uint16(timeNow>>12))   // set time_mid
	binary.BigEndian.PutUint16(u[6:], uint16(timeNow&0xfff)) // set time_low (minus four version bits)

	// Based on the RFC 9562 recommendation that this data be fully random and not a monotonic counter,
	// we do NOT support batching version 6 UUIDs.
	// set clock_seq (14 bits) and node (48 bits) pseudo-random bits (first 2 bits will be overridden)
	if _, err = io.ReadFull(g.rand, u[8:]); err != nil {
		return Nil, err
	}

	u.SetVersion(V6)

	// overwrite first 2 bits of byte[8] for the variant
	u.SetVariant(VariantRFC9562)

	return u, nil
}

// NewV7 returns a k-sortable UUID based on the current millisecond-precision
// UNIX epoch and 74 bits of pseudorandom data.
func (g *Gen) NewV7() (UUID, error) {
	return g.NewV7AtTime(g.epochFunc())
}

// NewV7 returns a k-sortable UUID based on the provided millisecond-precision
// UNIX epoch and 74 bits of pseudorandom data.
func (g *Gen) NewV7AtTime(atTime time.Time) (UUID, error) {
	var u UUID
	/* https://datatracker.ietf.org/doc/html/rfc9562#name-uuid-version-7
	    0                   1                   2                   3
	    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |                           unix_ts_ms                          |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |          unix_ts_ms           |  ver  |       rand_a          |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |var|                        rand_b                             |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	   |                            rand_b                             |
	   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ */

	ms, clockSeq, err := g.getClockSequence(true, atTime)
	if err != nil {
		return Nil, err
	}
	// UUIDv7 features a 48 bit timestamp. First 32bit (4bytes) represents seconds since 1970, followed by 2 bytes for the ms granularity.
	u[0] = byte(ms >> 40) // 1-6 bytes: big-endian unsigned number of Unix epoch timestamp
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)

	// Support batching by using a monotonic pseudo-random sequence,
	// as described in RFC 9562 section 6.2, Method 1.
	// The 6th byte contains the version and partially rand_a data.
	// We will lose the most significant bites from the clockSeq (with SetVersion), but it is ok,
	// we need the least significant that contains the counter to ensure the monotonic property
	binary.BigEndian.PutUint16(u[6:8], clockSeq) // set rand_a with clock seq which is random and monotonic

	// override first 4bits of u[6].
	u.SetVersion(V7)

	// set rand_b 64bits of pseudo-random bits (first 2 will be overridden)
	if _, err = io.ReadFull(g.rand, u[8:16]); err != nil {
		return Nil, err
	}
	// override first 2 bits of byte[8] for the variant
	u.SetVariant(VariantRFC9562)

	return u, nil
}

// GenerateBatchV7 creates a batch of k-sortable Version 7 UUIDs.
//
// Ensures strict monotonic ordering within the batch.
//
// Arguments:
// - batchSize: Number of UUIDs to generate.
//
// Returns:
// - []UUID: The generated UUIDs.
// - error: If batch generation fails.

func (g *MonotonicGen) GenerateBatchV7(batchSize int) ([]UUID, error) {
	if batchSize <= 0 {
		return nil, errors.New("batch size must be greater than zero")
	}

	uuids := make([]UUID, batchSize)

	for i := 0; i < batchSize; i++ {
		uuid, err := g.newMonotonicV7()
		if err != nil {
			return nil, err
		}
		uuids[i] = uuid
	}
	return uuids, nil
}

// newMonotonicV7 generates a Version 7 UUID with a monotonic counter for ordering.
//
// Returns:
// - UUID: The generated UUID.
// - error: If UUID generation fails.
func (g *MonotonicGen) newMonotonicV7() (UUID, error) {
	var u UUID

	ms, clockSeq, err := g.getMonotonicClockSequence(true, g.epochFunc())
	if err != nil {
		return Nil, err
	}

	// set the timestamp (48 bits)
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)

	// set rand_a (clockSeq ensures monotonicity)
	binary.BigEndian.PutUint16(u[6:8], clockSeq)

	// override version and variant bits
	u.SetVersion(V7)

	// set rand_b (64 random bits)
	if _, err := io.ReadFull(g.rand, u[8:16]); err != nil {
		return Nil, err
	}
	u.SetVariant(VariantRFC9562)

	return u, nil
}

// getClockSequence returns the epoch and clock sequence of the provided time,
// used for generating V1,V6 and V7 UUIDs.
//
// When useUnixTSMs is false, it uses the Coordinated Universal Time (UTC) as a count of
// 100-nanosecond intervals since 00:00:00.00, 15 October 1582 (the date of Gregorian
// reform to the Christian calendar).
func (g *Gen) getClockSequence(useUnixTSMs bool, atTime time.Time) (uint64, uint16, error) {
	var err error
	g.clockSequenceOnce.Do(func() {
		buf := make([]byte, 2)
		if _, err = io.ReadFull(g.rand, buf); err != nil {
			return
		}
		g.clockSequence = binary.BigEndian.Uint16(buf)
	})
	if err != nil {
		return 0, 0, err
	}

	g.storageMutex.Lock()
	defer g.storageMutex.Unlock()

	var timeNow uint64
	if useUnixTSMs {
		timeNow = uint64(atTime.UnixMilli())
	} else {
		timeNow = g.getEpoch(atTime)
	}
	// Clock didn't change since last UUID generation.
	// Should increase clock sequence.
	if timeNow <= g.lastTime {
		g.clockSequence++
	}
	g.lastTime = timeNow

	return timeNow, g.clockSequence, nil
}

// getMonotonicClockSequence returns a timestamp and clock sequence to ensure
// monotonic UUID generation, even when timestamps are identical.
//
// Arguments:
// - useUnixTSMs: Whether to use millisecond precision for the timestamp.
// - atTime: The reference time.
//
// Returns:
// - uint64: The timestamp.
// - uint16: The clock sequence.
// - error: If the sequence generation fails.
func (g *MonotonicGen) getMonotonicClockSequence(useUnixTSMs bool, atTime time.Time) (uint64, uint16, error) {
	g.monotonicMutex.Lock()
	defer g.monotonicMutex.Unlock()

	var timeNow uint64
	if useUnixTSMs {
		timeNow = uint64(atTime.UnixMilli())
	} else {
		timeNow = g.getEpoch(atTime)
	}

	// If timeNow <= lastTime, increment the counter to ensure monotonicity.
	if timeNow <= g.lastTime {
		g.monotonicCounter++
	} else {
		g.monotonicCounter = 0
	}

	g.lastTime = timeNow

	return timeNow, g.monotonicCounter, nil
}

// Returns the hardware address.
func (g *Gen) getHardwareAddr() ([]byte, error) {
	var err error
	g.hardwareAddrOnce.Do(func() {
		var hwAddr net.HardwareAddr
		if hwAddr, err = g.hwAddrFunc(); err == nil {
			copy(g.hardwareAddr[:], hwAddr)
			return
		}

		// Initialize hardwareAddr randomly in case
		// of real network interfaces absence.
		if _, err = io.ReadFull(g.rand, g.hardwareAddr[:]); err != nil {
			return
		}
		// Set multicast bit as recommended by RFC-9562
		g.hardwareAddr[0] |= 0x01
	})
	if err != nil {
		return []byte{}, err
	}
	return g.hardwareAddr[:], nil
}

// Returns the difference between UUID epoch (October 15, 1582)
// and the provided time in 100-nanosecond intervals.
func (g *Gen) getEpoch(atTime time.Time) uint64 {
	return epochStart + uint64(atTime.UnixNano()/100)
}

// Returns the UUID based on the hashing of the namespace UUID and name.
func newFromHash(h hash.Hash, ns UUID, name string) UUID {
	u := UUID{}
	h.Write(ns[:])
	h.Write([]byte(name))
	copy(u[:], h.Sum(nil))

	return u
}

var netInterfaces = net.Interfaces

// Returns the hardware address.
func defaultHWAddrFunc() (net.HardwareAddr, error) {
	ifaces, err := netInterfaces()
	if err != nil {
		return []byte{}, err
	}
	for _, iface := range ifaces {
		if len(iface.HardwareAddr) >= 6 {
			return iface.HardwareAddr, nil
		}
	}
	return []byte{}, ErrNoHwAddressFound
}
