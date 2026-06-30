from harness.mcp import McpServer, allowed_tools, resolve_env


def test_resolve_env_omits_unset_names():
    # An unset passthrough name must be OMITTED, not synthesized as "" — a blank
    # value masks a real inherited credential and hides "missing" as "empty".
    server = McpServer(command="gbrain", env_passthrough=["GBRAIN_TOKEN", "MISSING_VAR"])
    env = resolve_env(server, environ={"GBRAIN_TOKEN": "abc"})
    assert env == {"GBRAIN_TOKEN": "abc"}
    assert "MISSING_VAR" not in env


def test_allowed_tools_grants_each_server_prefix():
    servers = {"gbrain": McpServer(command="gbrain"), "composio": McpServer(command="composio")}
    assert set(allowed_tools(servers)) == {"mcp__gbrain", "mcp__composio"}
