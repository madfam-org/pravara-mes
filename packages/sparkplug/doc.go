// Package sparkplug implements the Eclipse Sparkplug 3.0 pieces that pravara's
// edge node and host application share: the topic namespace, payload
// encoding, sequence numbers, metric aliases, birth/death/data builders, DCMD
// parsing, the host STATE message and the per-edge-node ACL rules.
//
// The metric and command names follow pravara's MES-1 contract (see
// docs/sparkplug.md in this package's README).
package sparkplug
