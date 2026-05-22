using System.Globalization;
using System.IO;
using System.Windows;
using Hawkeye.Client.Models;
using Hawkeye.Client.Services;
using Microsoft.Win32;
using OpenCvSharp;
using OpenCvSharp.WpfExtensions;

namespace Hawkeye.Client;

public partial class MainWindow : System.Windows.Window
{
    private readonly ClientSettingsStore _settingsStore = new();
    private readonly EvidenceUploadService _uploadService = new();
    private readonly YoloDetectionService? _detector;
    private readonly object _latestFrameLock = new();

    private ClientSettings _settings;
    private VideoCapture? _capture;
    private Mat? _latestFrame;
    private CancellationTokenSource? _captureCts;
    private CancellationTokenSource? _uploadCts;
    private Task? _captureTask;
    private bool _isRunning;
    private DateTime _lastCaptureTime = DateTime.MinValue;
    private int _snapshotCount;
    private int _uploadSuccessCount;
    private int _uploadFailCount;

    public MainWindow()
    {
        InitializeComponent();

        _settings = _settingsStore.Load();
        LoadSettingsToUi();
        TxtConfigPath.Text = $"配置文件：{_settingsStore.SettingsPath}";

        try
        {
            _detector = new YoloDetectionService();
            TxtModelStatus.Text = _detector.IsReady
                ? $"模型：{_detector.ModelPath}"
                : "模型：未找到 yolov8s.onnx 或 yolov8n.onnx";
        }
        catch (Exception ex)
        {
            TxtModelStatus.Text = $"模型：初始化失败 - {ex.Message}";
        }

        UpdateButtons();
    }

    private void LoadSettingsToUi()
    {
        TxtServerUrl.Text = _settings.ServerUrl;
        TxtDeviceId.Text = _settings.DeviceId;
        TxtDeviceKey.Password = _settings.DeviceKey;
        TxtConfidence.Text = _settings.Confidence.ToString("0.00", CultureInfo.InvariantCulture);
        TxtFrameInterval.Text = _settings.DetectionFrameInterval.ToString(CultureInfo.InvariantCulture);
        TxtCaptureInterval.Text = _settings.CaptureIntervalSeconds.ToString(CultureInfo.InvariantCulture);
        TxtCameraIndex.Text = _settings.CameraIndex.ToString(CultureInfo.InvariantCulture);
    }

    private ClientSettings ReadSettingsFromUi()
    {
        var settings = new ClientSettings
        {
            ServerUrl = TxtServerUrl.Text,
            DeviceId = TxtDeviceId.Text,
            DeviceKey = TxtDeviceKey.Password,
            Confidence = ParseDouble(TxtConfidence.Text, _settings.Confidence),
            DetectionFrameInterval = ParseInt(TxtFrameInterval.Text, _settings.DetectionFrameInterval),
            CaptureIntervalSeconds = ParseInt(TxtCaptureInterval.Text, _settings.CaptureIntervalSeconds),
            CameraIndex = ParseInt(TxtCameraIndex.Text, _settings.CameraIndex),
            UploadTimeoutSeconds = _settings.UploadTimeoutSeconds
        };
        settings.Normalize();
        return settings;
    }

    private static int ParseInt(string value, int fallback)
        => int.TryParse(value, NumberStyles.Integer, CultureInfo.InvariantCulture, out var parsed) ? parsed : fallback;

    private static double ParseDouble(string value, double fallback)
        => double.TryParse(value, NumberStyles.Float, CultureInfo.InvariantCulture, out var parsed) ? parsed : fallback;

    private void BtnSaveSettings_Click(object sender, RoutedEventArgs e)
    {
        _settings = ReadSettingsFromUi();
        _settingsStore.Save(_settings);
        LoadSettingsToUi();
        TxtRuntimeStatus.Text = "配置已保存";
    }

    private void BtnCamera_Click(object sender, RoutedEventArgs e)
    {
        StartHawkeye(useFile: false);
    }

    private void BtnVideo_Click(object sender, RoutedEventArgs e)
    {
        var openFileDialog = new OpenFileDialog
        {
            Filter = "视频文件|*.mp4;*.avi;*.mkv|所有文件|*.*"
        };

        if (openFileDialog.ShowDialog() == true)
        {
            StartHawkeye(useFile: true, filePath: openFileDialog.FileName);
        }
    }

    private async void BtnStop_Click(object sender, RoutedEventArgs e)
    {
        await StopAsync();
    }

    private async void BtnTestUpload_Click(object sender, RoutedEventArgs e)
    {
        await UploadCurrentFrameAsync();
    }

    private void StartHawkeye(bool useFile, string filePath = "")
    {
        if (_isRunning)
        {
            return;
        }

        _settings = ReadSettingsFromUi();
        _settingsStore.Save(_settings);
        LoadSettingsToUi();

        try
        {
            var capture = useFile
                ? new VideoCapture(filePath)
                : new VideoCapture(_settings.CameraIndex, VideoCaptureAPIs.DSHOW);

            if (!capture.IsOpened())
            {
                capture.Dispose();
                MessageBox.Show("无法打开视频源。");
                return;
            }

            _capture = capture;
            _captureCts = new CancellationTokenSource();
            _uploadCts = new CancellationTokenSource();
            _isRunning = true;
            _lastCaptureTime = DateTime.MinValue;

            TxtRuntimeStatus.Text = useFile ? "视频演示运行中" : "摄像头运行中";
            TxtUploadStatus.Text = "上传：待机";
            TxtDetectionStatus.Text = "检测：等待画面";
            UpdateButtons();

            var runSettings = ReadSettingsFromUi();
            _captureTask = Task.Run(() => CaptureLoopAsync(capture, runSettings, _captureCts.Token));
        }
        catch (Exception ex)
        {
            MessageBox.Show($"启动出错: {ex.Message}");
            _isRunning = false;
            UpdateButtons();
        }
    }

    private async Task StopAsync()
    {
        if (!_isRunning && _captureTask == null)
        {
            CameraView.Source = null;
            return;
        }

        _isRunning = false;
        _captureCts?.Cancel();
        _uploadCts?.Cancel();

        var task = _captureTask;
        if (task != null)
        {
            try
            {
                await task;
            }
            catch (OperationCanceledException)
            {
            }
        }

        _captureTask = null;
        _captureCts?.Dispose();
        _captureCts = null;
        _uploadCts?.Dispose();
        _uploadCts = null;
        CameraView.Source = null;
        lock (_latestFrameLock)
        {
            _latestFrame?.Dispose();
            _latestFrame = null;
        }
        TxtRuntimeStatus.Text = "已停止";
        UpdateButtons();
    }

    private async Task CaptureLoopAsync(VideoCapture capture, ClientSettings settings, CancellationToken token)
    {
        var snapshotFolder = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "Snapshots");
        Directory.CreateDirectory(snapshotFolder);

        using var frame = new Mat();
        var frameCount = 0L;
        var lastResults = new List<DetectionResult>();

        try
        {
            while (!token.IsCancellationRequested)
            {
                if (!capture.Read(frame) || frame.Empty())
                {
                    break;
                }

                frameCount++;
                StoreLatestFrame(frame);
                var personCount = 0;
                var snapshotTaken = false;

                if (frameCount % settings.DetectionFrameInterval == 0 && _detector?.IsReady == true)
                {
                    try
                    {
                        lastResults = _detector.Detect(frame, settings.Confidence);
                    }
                    catch (Exception ex)
                    {
                        await Dispatcher.InvokeAsync(() => TxtDetectionStatus.Text = $"检测：失败 - {ex.Message}");
                    }
                }

                foreach (var item in lastResults)
                {
                    if (!string.Equals(item.Label, "person", StringComparison.OrdinalIgnoreCase))
                    {
                        continue;
                    }

                    personCount++;
                    Cv2.Rectangle(frame, item.BoundingBox, Scalar.Red, 2);
                }

                if (personCount > 0 && DateTime.Now - _lastCaptureTime > TimeSpan.FromSeconds(settings.CaptureIntervalSeconds))
                {
                    var fileName = $"Evidence_{DateTime.Now:yyyyMMdd_HHmmss_fff}.jpg";
                    var fullPath = Path.Combine(snapshotFolder, fileName);

                    frame.SaveImage(fullPath);
                    _lastCaptureTime = DateTime.Now;
                    _snapshotCount++;
                    snapshotTaken = true;

                    var uploadToken = _uploadCts?.Token ?? CancellationToken.None;
                    _ = UploadSnapshotAsync(fullPath, settings, uploadToken);
                }

                DrawOverlay(frame, personCount, snapshotTaken);
                await Dispatcher.InvokeAsync(() =>
                {
                    CameraView.Source = frame.ToWriteableBitmap();
                    TxtDetectionStatus.Text = $"检测：person={personCount} frame={frameCount}";
                    TxtSnapshotStatus.Text = $"抓拍：{_snapshotCount}";
                    BtnTestUpload.IsEnabled = true;
                });

                await Task.Delay(1, token);
            }
        }
        finally
        {
            capture.Release();
            capture.Dispose();
            await Dispatcher.InvokeAsync(() =>
            {
                if (ReferenceEquals(_capture, capture))
                {
                    _capture = null;
                }

                _isRunning = false;
                TxtRuntimeStatus.Text = token.IsCancellationRequested ? "已停止" : "视频源已结束";
                UpdateButtons();
            });
        }
    }

    private async Task UploadSnapshotAsync(string filePath, ClientSettings settings, CancellationToken token)
    {
        await Dispatcher.InvokeAsync(() => TxtUploadStatus.Text = $"上传：{Path.GetFileName(filePath)}");

        var result = await _uploadService.UploadAsync(filePath, settings, token);
        if (result.Success)
        {
            _uploadSuccessCount++;
        }
        else
        {
            _uploadFailCount++;
        }

        await Dispatcher.InvokeAsync(() =>
        {
            TxtUploadStatus.Text = $"上传：成功 {_uploadSuccessCount} / 失败 {_uploadFailCount}；{result.Message}";
        });
    }

    private static void DrawOverlay(Mat frame, int personCount, bool snapshotTaken)
    {
        var statusText = $"CROWD: {personCount}";
        Cv2.Rectangle(frame, new OpenCvSharp.Rect(0, 0, 400, 60), Scalar.Black, -1);
        Cv2.PutText(
            frame,
            statusText,
            new OpenCvSharp.Point(10, 45),
            HersheyFonts.HersheyComplex,
            1.2,
            personCount > 0 ? Scalar.Red : Scalar.Green,
            2);

        if (!snapshotTaken)
        {
            return;
        }

        Cv2.Circle(frame, new OpenCvSharp.Point(380, 30), 15, Scalar.Red, -1);
        Cv2.PutText(frame, "UPLOADING...", new OpenCvSharp.Point(410, 45), HersheyFonts.HersheyComplex, 0.7, Scalar.Red, 2);
    }

    private void UpdateButtons()
    {
        BtnCamera.IsEnabled = !_isRunning;
        BtnVideo.IsEnabled = !_isRunning;
        BtnSaveSettings.IsEnabled = !_isRunning;
                BtnStop.IsEnabled = _isRunning;
        BtnTestUpload.IsEnabled = _latestFrame != null;
    }

    private void StoreLatestFrame(Mat frame)
    {
        lock (_latestFrameLock)
        {
            _latestFrame?.Dispose();
            _latestFrame = frame.Clone();
        }
    }

    private async Task UploadCurrentFrameAsync()
    {
        Mat? snapshot;
        lock (_latestFrameLock)
        {
            snapshot = _latestFrame?.Clone();
        }

        if (snapshot == null || snapshot.Empty())
        {
            TxtUploadStatus.Text = "上传：当前没有可上传画面";
            snapshot?.Dispose();
            return;
        }

        try
        {
            _settings = ReadSettingsFromUi();
            _settingsStore.Save(_settings);

            var snapshotFolder = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "Snapshots");
            Directory.CreateDirectory(snapshotFolder);
            var fileName = $"Manual_{DateTime.Now:yyyyMMdd_HHmmss_fff}.jpg";
            var fullPath = Path.Combine(snapshotFolder, fileName);
            snapshot.SaveImage(fullPath);
            _snapshotCount++;
            TxtSnapshotStatus.Text = $"抓拍：{_snapshotCount}";

            using var uploadCts = new CancellationTokenSource();
            await UploadSnapshotAsync(fullPath, _settings, uploadCts.Token);
        }
        finally
        {
            snapshot.Dispose();
        }
    }

    protected override async void OnClosed(EventArgs e)
    {
        await StopAsync();
        base.OnClosed(e);
    }
}
