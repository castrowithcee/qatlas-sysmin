// Package execute runs the resolved steps of a project: it carries out each building block through a
// Runner, limits each step by its time limit and its output size, stops at the first failure and writes a
// run log. A dry run lists what would be done without touching anything.
//
// Only the command block passes a command line through a shell. Every other block hands its parameters to
// the external program as separate arguments.
package execute
