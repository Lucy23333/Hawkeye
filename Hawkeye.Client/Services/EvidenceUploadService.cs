using System.IO;
using System.Net.Http;
using Hawkeye.Client.Models;

namespace Hawkeye.Client.Services;

public sealed class EvidenceUploadService
{
    private static readonly HttpClient Client = new();

    public async Task<UploadResult> UploadAsync(string filePath, ClientSettings settings, CancellationToken cancellationToken)
    {
        settings.Normalize();
        if (string.IsNullOrWhiteSpace(settings.ServerUrl))
        {
            return UploadResult.Failed("服务地址为空");
        }

        if (string.IsNullOrWhiteSpace(settings.DeviceKey))
        {
            return UploadResult.Failed("设备密钥为空，后端会拒绝上传");
        }

        try
        {
            using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
            timeout.CancelAfter(TimeSpan.FromSeconds(settings.UploadTimeoutSeconds));

            await using var stream = File.OpenRead(filePath);
            using var content = new MultipartFormDataContent();
            using var fileContent = new StreamContent(stream);
            content.Add(fileContent, "image", Path.GetFileName(filePath));
            content.Add(new StringContent(settings.DeviceId), "device_id");

            using var request = new HttpRequestMessage(HttpMethod.Post, settings.ServerUrl);
            request.Headers.TryAddWithoutValidation("X-Device-Key", settings.DeviceKey);
            request.Content = content;

            using var response = await Client.SendAsync(request, timeout.Token);
            var responseBody = await response.Content.ReadAsStringAsync(timeout.Token);
            if (response.IsSuccessStatusCode)
            {
                return new UploadResult(true, string.IsNullOrWhiteSpace(responseBody) ? "上传成功" : responseBody);
            }

            var detail = string.IsNullOrWhiteSpace(responseBody) ? response.ReasonPhrase : responseBody.Trim();
            return UploadResult.Failed($"HTTP {(int)response.StatusCode} {detail}");
        }
        catch (OperationCanceledException) when (!cancellationToken.IsCancellationRequested)
        {
            return UploadResult.Failed("上传超时");
        }
        catch (Exception ex)
        {
            return UploadResult.Failed(ex.Message);
        }
    }
}

public sealed record UploadResult(bool Success, string Message)
{
    public static UploadResult Succeeded() => new(true, "上传成功");
    public static UploadResult Failed(string message) => new(false, message);
}
