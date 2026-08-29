import { useEffect, useRef, useState } from 'react';
import { Camera, CameraOff, AlertCircle } from 'lucide-react';

export function CameraPreview({ className }: { className?: string }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [cameraOn, setCameraOn] = useState(true);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    let stream: MediaStream | null = null;
    let cancelled = false;
    setErrorMsg(null);

    if (cameraOn) {
      if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        setErrorMsg("HTTPS required or device not found");
        return;
      }
      navigator.mediaDevices.getUserMedia({ video: true })
        .then(s => {
          // cameraOn may have flipped to false (or the component unmounted)
          // while the permission prompt was pending — don't attach a stream
          // the UI is already showing as "off".
          if (cancelled) {
            s.getTracks().forEach(track => track.stop());
            return;
          }
          stream = s;
          if (videoRef.current) {
             videoRef.current.srcObject = stream;
          }
        })
        .catch(err => {
          if (!cancelled) {
            console.error("Camera access denied", err);
            setErrorMsg("Permission denied");
          }
        });
    } else {
       if (videoRef.current && videoRef.current.srcObject) {
         const s = videoRef.current.srcObject as MediaStream;
         s.getTracks().forEach(track => track.stop());
         videoRef.current.srcObject = null;
       }
    }

    return () => {
      cancelled = true;
      if (stream) {
        stream.getTracks().forEach(track => track.stop());
      }
    };
  }, [cameraOn]);

  return (
    <div className={`relative flex flex-col items-center justify-center bg-ink rounded-2xl overflow-hidden border border-line-strong shadow-xl group ${className || 'w-full h-full'}`}>
      <video 
        ref={videoRef} 
        autoPlay 
        muted 
        playsInline 
        className={`absolute inset-0 w-full h-full object-cover transform -scale-x-100 ${(!cameraOn || errorMsg) ? 'opacity-0' : 'opacity-100'}`}
      />
      
      {errorMsg && (
         <div className="absolute inset-0 flex flex-col items-center justify-center text-red-400 bg-red-900/20 p-4 text-center z-10">
           <AlertCircle className="w-8 h-8 mb-2" />
           <span className="text-sm font-medium">{errorMsg}</span>
         </div>
      )}

      {(!cameraOn && !errorMsg) && (
         <div className="absolute inset-0 flex items-center justify-center text-ink-faint bg-ink z-10">
           <CameraOff className="w-12 h-12" />
         </div>
      )}
      
      {/* Controls Overlay */}
      <div className="absolute bottom-4 inset-x-0 flex justify-center z-20">
         <button 
           onClick={() => setCameraOn(!cameraOn)}
           className={`p-3 rounded-full text-white backdrop-blur-md transition-colors shadow-lg ${cameraOn ? 'bg-black/40 hover:bg-black/60' : 'bg-red-500 hover:bg-red-600'}`}
         >
           {cameraOn ? <Camera className="w-5 h-5" /> : <CameraOff className="w-5 h-5" />}
         </button>
      </div>

      <div className="absolute top-4 left-4 z-20">
        <span className="px-2 py-1 bg-black/40 backdrop-blur-md rounded text-[10px] font-mono font-medium text-white/90 tracking-wider">
          YOU
        </span>
      </div>
    </div>
  );
}
