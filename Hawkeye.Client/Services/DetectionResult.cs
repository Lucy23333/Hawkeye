using OpenCvSharp;

namespace Hawkeye.Client.Services;

public sealed record DetectionResult(string Label, Rect BoundingBox);
