// ocr prints the text Apple's Vision framework reads in each image, one line
// per piece of text: the file, a tab, the text. docs/demo/record.sh scans it
// for names and paths from outside the sandbox. It runs on the Mac alone,
// with nothing sent anywhere.
//
//   ocr IMAGE...
import Foundation
import ImageIO
import Vision

for path in CommandLine.arguments.dropFirst() {
    guard let source = CGImageSourceCreateWithURL(URL(fileURLWithPath: path) as CFURL, nil),
          let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
        FileHandle.standardError.write(Data("ocr: cannot read \(path)\n".utf8))
        exit(1)
    }
    let request = VNRecognizeTextRequest()
    request.recognitionLevel = .accurate
    // Paths and names are not words: leave them as they are drawn.
    request.usesLanguageCorrection = false
    do {
        try VNImageRequestHandler(cgImage: image).perform([request])
    } catch {
        FileHandle.standardError.write(Data("ocr: \(path): \(error.localizedDescription)\n".utf8))
        exit(1)
    }
    for observation in request.results ?? [] {
        if let text = observation.topCandidates(1).first?.string {
            print("\(path)\t\(text)")
        }
    }
}
