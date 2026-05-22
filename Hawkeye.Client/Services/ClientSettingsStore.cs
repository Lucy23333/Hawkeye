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
            Save(created);
            return created;
        }

        try
        {
            var json = File.ReadAllText(SettingsPath);
            var settings = JsonSerializer.Deserialize<ClientSettings>(json) ?? new ClientSettings();
            settings.Normalize();
            return settings;
        }
        catch
        {
            return new ClientSettings();
        }
    }

    public void Save(ClientSettings settings)
    {
        settings.Normalize();
        Directory.CreateDirectory(AppContext.BaseDirectory);
        File.WriteAllText(SettingsPath, JsonSerializer.Serialize(settings, _jsonOptions));
    }
}
