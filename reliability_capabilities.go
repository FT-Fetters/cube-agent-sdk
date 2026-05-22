package agent

type reliableCapabilityModel struct {
	*reliableModel
	capabilities ModelCapabilities
}

func (m *reliableCapabilityModel) Capabilities() ModelCapabilities {
	if m == nil {
		return ModelCapabilities{}
	}
	return m.capabilities
}

type reliableCapabilityStreamModel struct {
	*reliableStreamModel
	capabilities ModelCapabilities
}

func (m *reliableCapabilityStreamModel) Capabilities() ModelCapabilities {
	if m == nil {
		return ModelCapabilities{}
	}
	return m.capabilities
}
