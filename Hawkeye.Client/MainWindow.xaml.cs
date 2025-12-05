using System;
using System.IO;
using System.Linq;
using System.Net.Http; // 🔥 新增：网络通信库
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using Microsoft.Win32;
using OpenCvSharp;
using OpenCvSharp.WpfExtensions;
using YoloDotNet;
using YoloDotNet.Models;
using SkiaSharp;

namespace Hawkeye.Client
{
    public partial class MainWindow : System.Windows.Window
    {
        private VideoCapture _capture;
        private bool _isRunning = false;
        private CancellationTokenSource _cts;
        private Yolo _yolo;

        // 抓拍相关
        private DateTime _lastCaptureTime = DateTime.MinValue;
        private TimeSpan _captureInterval = TimeSpan.FromSeconds(3); // 改成 3秒，防止传太快服务器受不了

        // 🔥 新增：专门负责上传的快递员 (HttpClient)
        private static readonly HttpClient client = new HttpClient();

        // 🔥 请确认这个 IP 是你 Ubuntu 的 IP！
        // ❌ 以前是: "http://192.168.153.131:8080/upload"
        // ✅ 改成公网 (注意后面要带 /upload):
        private const string SERVER_URL = " https://2dab6389.r27.cpolar.top/upload";

        public MainWindow()
        {
            InitializeComponent();
            InitializeAI();
        }

        private void InitializeAI()
        {
            try
            {
                string modelPath = File.Exists("yolov8s.onnx") ? "yolov8s.onnx" : "yolov8n.onnx";
                var options = new YoloOptions { OnnxModel = modelPath };
                _yolo = new Yolo(options);
            }
            catch (Exception ex)
            {
                MessageBox.Show($"AI 初始化失败: {ex.Message}");
            }
        }

        private void BtnCamera_Click(object sender, RoutedEventArgs e)
        {
            StartHawkeye(useFile: false);
        }

        private void BtnVideo_Click(object sender, RoutedEventArgs e)
        {
            OpenFileDialog openFileDialog = new OpenFileDialog();
            openFileDialog.Filter = "视频文件|*.mp4;*.avi;*.mkv|所有文件|*.*";
            if (openFileDialog.ShowDialog() == true)
            {
                StartHawkeye(useFile: true, filePath: openFileDialog.FileName);
            }
        }

        private void StartHawkeye(bool useFile, string filePath = "")
        {
            if (_isRunning) return;

            try
            {
                if (useFile)
                    _capture = new VideoCapture(filePath);
                else
                    _capture = new VideoCapture(0, VideoCaptureAPIs.DSHOW);

                if (!_capture.IsOpened())
                {
                    MessageBox.Show("无法打开视频源！");
                    return;
                }

                _isRunning = true;
                _cts = new CancellationTokenSource();
                Task.Run(() => CaptureLoop(_cts.Token));
            }
            catch (Exception ex)
            {
                MessageBox.Show($"启动出错: {ex.Message}");
            }
        }

        private void BtnStop_Click(object sender, RoutedEventArgs e)
        {
            _isRunning = false;
            _cts?.Cancel();
            Thread.Sleep(100);
            _capture?.Release();
            _capture = null;
            CameraView.Source = null;
        }

        private void CaptureLoop(CancellationToken token)
        {
            // 确保截图文件夹存在
            string snapshotFolder = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "Snapshots");
            if (!Directory.Exists(snapshotFolder)) Directory.CreateDirectory(snapshotFolder);

            using (Mat frame = new Mat())
            {
                // 🔥 优化变量 1：帧计数器
                long frameCount = 0;

                // 🔥 优化变量 2：缓存上一次的 AI 结果
                // 这样在 AI 休息的时候，我们依然能画出框框，不会闪烁
                System.Collections.Generic.List<ObjectDetection> lastResults = new System.Collections.Generic.List<ObjectDetection>();

                while (_isRunning && !token.IsCancellationRequested)
                {
                    _capture.Read(frame);
                    if (frame.Empty()) break;

                    frameCount++; // 每读一帧，计数+1

                    int personCount = 0;
                    bool isSnapshotTaken = false;

                    // --- 🧠 AI 识别 (跳帧优化版) ---
                    // 只有当帧数是 3 的倍数时 (1, 4, 7...) 才跑 AI
                    // 这里的 '3' 可以改：如果还卡就改成 5，如果不卡可以改成 2
                    if (frameCount % 3 == 0)
                    {
                        if (_yolo != null)
                        {
                            try
                            {
                                var data = frame.CvtColor(ColorConversionCodes.BGR2RGB).ToBytes(".jpg");
                                using (var skImage = SKImage.FromEncodedData(data))
                                {
                                    // 真正跑 AI
                                    var results = _yolo.RunObjectDetection(skImage, confidence: 0.25);

                                    // 更新缓存
                                    lastResults = results;
                                }
                            }
                            catch { }
                        }
                    }
                    // -----------------------------

                    // --- 🎨 绘制 (每一帧都画，用的是 lastResults) ---
                    foreach (var item in lastResults)
                    {
                        if (item.Label.Name == "person")
                        {
                            personCount++;
                            var rect = new OpenCvSharp.Rect((int)item.BoundingBox.Left, (int)item.BoundingBox.Top, (int)item.BoundingBox.Width, (int)item.BoundingBox.Height);
                            Cv2.Rectangle(frame, rect, Scalar.Red, 2);
                        }
                    }

                    // --- 📸 抓拍 & 上传 (逻辑不变) ---
                    if (personCount > 0)
                    {
                        if ((DateTime.Now - _lastCaptureTime).TotalSeconds > 1)
                        {
                            // 声音还是会有，证明系统在工作
                            // System.Media.SystemSounds.Hand.Play(); // 嫌吵可以注释掉
                        }

                        if (DateTime.Now - _lastCaptureTime > _captureInterval)
                        {
                            string fileName = $"Evidence_{DateTime.Now:yyyyMMdd_HHmmss_fff}.jpg";
                            string fullPath = Path.Combine(snapshotFolder, fileName);

                            frame.SaveImage(fullPath);
                            Task.Run(() => UploadImageToUbuntu(fullPath));

                            _lastCaptureTime = DateTime.Now;
                            isSnapshotTaken = true;
                            Console.WriteLine($"[抓拍] {fileName}");
                        }
                    }

                    // UI 绘制
                    string statusText = $"CROWD: {personCount}";
                    Cv2.Rectangle(frame, new OpenCvSharp.Rect(0, 0, 400, 60), Scalar.Black, -1);
                    Cv2.PutText(frame, statusText, new OpenCvSharp.Point(10, 45), HersheyFonts.HersheyComplex, 1.2, personCount > 0 ? Scalar.Red : Scalar.Green, 2);

                    if (isSnapshotTaken || (DateTime.Now - _lastCaptureTime).TotalSeconds < 0.5)
                    {
                        Cv2.Circle(frame, new OpenCvSharp.Point(380, 30), 15, Scalar.Red, -1);
                        Cv2.PutText(frame, "UPLOADING...", new OpenCvSharp.Point(410, 45), HersheyFonts.HersheyComplex, 0.7, Scalar.Red, 2);
                    }

                    Dispatcher.Invoke(() => CameraView.Source = frame.ToWriteableBitmap());

                    // 🔥 优化变量 3：减少休眠时间，让它跑得跟视频一样快
                    // 之前是 10ms，现在 AI 跑得少了，我们可以让 UI 刷新更快点
                    Thread.Sleep(1);
                }
            }
            Dispatcher.Invoke(() => { if (_isRunning) BtnStop_Click(null, null); });
        }

        // --- 🔥 核心上传函数 ---
        private async Task UploadImageToUbuntu(string filePath)
        {
            try
            {
                using (var content = new MultipartFormDataContent())
                {
                    byte[] fileBytes = File.ReadAllBytes(filePath);
                    var fileContent = new ByteArrayContent(fileBytes);
                    content.Add(fileContent, "image", Path.GetFileName(filePath));

                    // 发送 POST 请求
                    var response = await client.PostAsync(SERVER_URL, content);

                    if (response.IsSuccessStatusCode)
                    {
                        Console.WriteLine("✅ 上传成功！Ubuntu 已接收。");
                    }
                    else
                    {
                        Console.WriteLine($"❌ 上传失败: {response.StatusCode}");
                    }
                }
            }
            catch (Exception ex)
            {
                Console.WriteLine($"❌ 网络错误 (Ubuntu 没开?): {ex.Message}");
            }
        }

        protected override void OnClosed(EventArgs e)
        {
            BtnStop_Click(null, null);
            base.OnClosed(e);
        }
    }
}   