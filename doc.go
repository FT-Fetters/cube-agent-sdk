// Package agent provides a small SDK for building agents with managed context.
//
// Developers define a system prompt and attach optional capabilities such as
// skills, MCP server configuration, tools, approval policies, hooks, context
// compaction, and parent-controlled subagents. The package intentionally keeps
// model providers and MCP process management behind interfaces so applications
// can integrate their own infrastructure.
package agent
