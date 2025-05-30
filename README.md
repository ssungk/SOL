# 🚀 SOL

`Stream On Live`는 Go 언어로 구축된 개인 미디어 서버입니다. 이 프로젝트는 RTMP(Real-Time Messaging Protocol) 스트림을 수신하고, 이를 HLS(HTTP Live Streaming) 및 RTSP(Real Time Streaming Protocol) 같은 다양한 프로토콜로 재배포하는 기능을 제공합니다.

라이브 스트리밍, 비디오 온 디맨드(VOD) 등 다양한 미디어 콘텐츠를 효율적으로 처리하고 전송하는 데 중점을 둡니다.

## ✨ 주요 기능

* **RTMP Ingest:** RTMP 프로토콜을 통한 라이브 스트림 수신.
* **HLS Transcoding & Distribution:** 수신된 RTMP 스트림을 Apple HLS 형식으로 변환하여 웹 및 모바일 장치에서 재생 가능하도록 제공.
* **RTSP Distribution:** RTSP 프로토콜을 통한 스트림 재배포.
* **간결한 아키텍처:** Go 언어의 동시성 모델을 활용하여 높은 성능과 안정성을 목표로 설계.
* **쉬운 확장성:** 모듈식 설계를 통해 향후 기능 추가 및 확장이 용이.

## 🛠️ 기술 스택

* **Go:** 서버 로직 구현
* **RTMP:** 스트림 수신 프로토콜
* **HLS:** HTTP 기반 스트림 전송 프로토콜
* **RTSP:** 실시간 스트림 전송 프로토콜
 

## 📄 라이선스
* 이 프로젝트는 MIT 라이선스에 따라 배포됩니다.