package agent

func cloneMCPServers(servers []MCPServerConfig) []MCPServerConfig {
	if len(servers) == 0 {
		return nil
	}
	cloned := make([]MCPServerConfig, len(servers))
	for i, server := range servers {
		cloned[i] = server
		cloned[i].Args = append([]string(nil), server.Args...)
		if len(server.Env) > 0 {
			cloned[i].Env = make(map[string]string, len(server.Env))
			for key, value := range server.Env {
				cloned[i].Env[key] = value
			}
		}
	}
	return cloned
}
