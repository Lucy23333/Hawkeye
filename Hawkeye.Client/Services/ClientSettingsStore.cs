using System.IO;
using System.Text.Json;
using Hawkeye.Client.Models;

namespace Hawkeye.Client.Services;

public sealed class ClientSettingsStore
{
    private const string FileName = "clientsettings.json";
    private readonly JsonSerializerOptions _jsonOptions = new() { WriteIndented = true };

    public string SettingsPath => Path.Combine(AppContext.BaseDirectory, FileName);

    public ClientSettings Load()
    {
        if (!File.Exists(SettingsPath))
        {
            var created = new ClientSettings();
            ApplyBackendDefaults(created);
            Save(created);
            return created;
        }

        try
        {
            var json = File.ReadAllText(SettingsPath);
            var settings = JsonSerializer.Deserialize<ClientSettings>(json) ?? new ClientSettings();
            settings.Normalize();
            if (ApplyBackendDefaults(settings))
            {
                Save(settings);
            }
            return settings;
        }
        catch
        {
            var fallback = new ClientSettings();
            ApplyBackendDefaults(fallback);
            return fallback;
        }
    }

    public void Save(ClientSettings settings)
    {
        settings.Normalize();
        Directory.CreateDirectory(AppContext.BaseDirectory);
        File.WriteAllText(SettingsPath, JsonSerializer.Serialize(settings, _jsonOptions));
    }

    private static bool ApplyBackendDefaults(ClientSettings settings)
    {
        if (!string.IsNullOrWhiteSpace(settings.DeviceKey))
        {
            return false;
        }

        var backendConfigPath = FindBackendConfigPath();
        if (backendConfigPath == null)
        {
            return false;
        }

        try
        {
            using var doc = JsonDocument.Parse(File.ReadAllText(backendConfigPath));
            if (!doc.RootElement.TryGetProperty("device_key", out var deviceKeyElement))
            {
                return false;
            }

            var deviceKey = deviceKeyElement.GetString();
            if (string.IsNullOrWhiteSpace(deviceKey))
            {
                return false;
            }

            settings.DeviceKey = deviceKey;
            settings.Normalize();
            return true;
        }
        catch
        {
            return false;
        }
    }

    private static string? FindBackendConfigPath()
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir != null)
        {
            var candidate = Path.Combine(dir.FullName, "hawkeye-backend", "config.json");
            if (File.Exists(candidate))
            {
                return candidate;
            }

            dir = dir.Parent;
        }

        return null;
    }
}
