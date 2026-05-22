namespace Hawkeye.Client.Models;

public sealed class ClientSettings
{
    public string ServerUrl { get; set; } = "http://127.0.0.1:8080/upload";
    public string DeviceId { get; set; } = "CAM-01";
    public string DeviceKey { get; set; } = "";
    public int CameraIndex { get; set; } = 0;
    public int DetectionFrameInterval { get; set; } = 3;
    public double Confidence { get; set; } = 0.25;
    public int CaptureIntervalSeconds { get; set; } = 3;
    public int UploadTimeoutSeconds { get; set; } = 15;

    public void Normalize()
    {
        ServerUrl = (ServerUrl ?? "").Trim();
        DeviceId = string.IsNullOrWhiteSpace(DeviceId) ? "CAM-01" : DeviceId.Trim();
        DeviceKey = (DeviceKey ?? "").Trim();
        CameraIndex = Math.Max(0, CameraIndex);
        DetectionFrameInterval = Math.Clamp(DetectionFrameInterval, 1, 30);
        Confidence = Math.Clamp(Confidence, 0.05, 0.95);
        CaptureIntervalSeconds = Math.Clamp(CaptureIntervalSeconds, 1, 3600);
        UploadTimeoutSeconds = Math.Clamp(UploadTimeoutSeconds, 3, 120);
    }
}
