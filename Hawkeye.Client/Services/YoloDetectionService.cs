using System.IO;
using OpenCvSharp;
using SkiaSharp;
using YoloDotNet;
using YoloDotNet.Models;

namespace Hawkeye.Client.Services;

public sealed class YoloDetectionService
{
    private readonly Yolo? _yolo;

    public bool IsReady => _yolo != null;
    public string ModelPath { get; }

    public YoloDetectionService()
    {
        ModelPath = File.Exists("yolov8s.onnx") ? "yolov8s.onnx" : "yolov8n.onnx";
        if (!File.Exists(ModelPath))
        {
            return;
        }

        var options = new YoloOptions { OnnxModel = ModelPath };
        _yolo = new Yolo(options);
    }

    public List<DetectionResult> Detect(Mat frame, double confidence)
    {
        if (_yolo == null)
        {
            return [];
        }

        var data = frame.CvtColor(ColorConversionCodes.BGR2RGB).ToBytes(".jpg");
        using var skImage = SKImage.FromEncodedData(data);
        var results = _yolo.RunObjectDetection(skImage, confidence);

        return results
            .Select(item => new DetectionResult(
                item.Label.Name,
                new Rect(
                    (int)item.BoundingBox.Left,
                    (int)item.BoundingBox.Top,
                    (int)item.BoundingBox.Width,
                    (int)item.BoundingBox.Height)))
            .ToList();
    }
}
