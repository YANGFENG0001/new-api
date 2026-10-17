package model_setting

import (
	"bytes"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldPreserveThinkingSuffixExactAndRegex(t *testing.T) {
	settings := GetGlobalSettings()
	original := append([]string(nil), settings.ThinkingModelBlacklist...)
	t.Cleanup(func() { settings.ThinkingModelBlacklist = original })

	assert.True(t, ShouldPreserveThinkingSuffix("kimi-k2-thinking"))
	assert.True(t, ShouldPreserveThinkingSuffix("moonshotai/kimi-k2-thinking"))
	assert.False(t, ShouldPreserveThinkingSuffix("m@sha256:abc"))

	settings.ThinkingModelBlacklist = []string{
		"kimi-k2-thinking",
		"re:[",
		"re:",
		"re:.*@sha256:.*",
	}

	var logged bytes.Buffer
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logged
	t.Cleanup(func() { gin.DefaultErrorWriter = previous })

	assert.True(t, ShouldPreserveThinkingSuffix("kimi-k2-thinking"))
	assert.True(t, ShouldPreserveThinkingSuffix("m@sha256:abc"))
	assert.False(t, ShouldPreserveThinkingSuffix("m@sha256"))
	assert.False(t, ShouldPreserveThinkingSuffix("qwen3-max@thinking:on"))
	require.Contains(t, logged.String(), `invalid thinking_model_blacklist regex "re:["`)
	require.Contains(t, logged.String(), `invalid thinking_model_blacklist regex "re:"`)

	settings.ThinkingModelBlacklist = []string{"re:^beta@"}
	assert.False(t, ShouldPreserveThinkingSuffix("m@sha256:abc"))
	assert.True(t, ShouldPreserveThinkingSuffix("beta@sha256:abc"))
	assert.False(t, ShouldPreserveThinkingSuffix("alpha@sha256:abc"))
}

func TestShouldSanitizeResponsesEncryptedContent(t *testing.T) {
	settings := GetGlobalSettings()
	original := settings.ResponsesSanitizeEncryptedContent
	t.Cleanup(func() { settings.ResponsesSanitizeEncryptedContent = original })

	t.Setenv(responsesSanitizeEnvVar, "")

	settings.ResponsesSanitizeEncryptedContent = true
	require.True(t, ShouldSanitizeResponsesEncryptedContent())
	settings.ResponsesSanitizeEncryptedContent = false
	require.False(t, ShouldSanitizeResponsesEncryptedContent())

	for _, value := range []string{"1", "true", "ON", " yes "} {
		t.Setenv(responsesSanitizeEnvVar, value)
		settings.ResponsesSanitizeEncryptedContent = false
		require.True(t, ShouldSanitizeResponsesEncryptedContent(), "env=%q", value)
	}
	for _, value := range []string{"0", "false", "Off", "no"} {
		t.Setenv(responsesSanitizeEnvVar, value)
		settings.ResponsesSanitizeEncryptedContent = true
		require.False(t, ShouldSanitizeResponsesEncryptedContent(), "env=%q", value)
	}

	t.Setenv(responsesSanitizeEnvVar, "auto")
	settings.ResponsesSanitizeEncryptedContent = false
	require.False(t, ShouldSanitizeResponsesEncryptedContent())
	settings.ResponsesSanitizeEncryptedContent = true
	require.True(t, ShouldSanitizeResponsesEncryptedContent())
}

func TestResponsesSanitizeOptionExported(t *testing.T) {
	exported := config.GlobalConfig.ExportAllConfigs()
	value, ok := exported["global.responses_sanitize_encrypted_content"]
	require.True(t, ok, "option global.responses_sanitize_encrypted_content is not registered")
	require.Contains(t, []string{"true", "false"}, value)
}
