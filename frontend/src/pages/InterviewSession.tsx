import { useState, useEffect, useRef } from 'react';
import { Mic, SkipForward, VolumeX, Send, Loader2, Volume2, MicOff, FileCode2 } from 'lucide-react';
import { useSearchParams, useParams, useNavigate } from 'react-router-dom';
import { CameraPreview } from '../components/CameraPreview';
import { readTarget, targetApiParams } from '../lib/interviewTarget';

export function InterviewSession() {
  const { repoId } = useParams();
  const [searchParams] = useSearchParams();
  const isVoiceMode = searchParams.get('mode') === 'voice';
  // The Job Map role or company this interview is practice for, carried here
  // from the directory. Ids only — the backend resolves them.
  const targetParams = targetApiParams(readTarget(searchParams.toString()));
  const navigate = useNavigate();

  const [questions, setQuestions] = useState<any[]>([]);
  const [currentQuestionIdx, setCurrentQuestionIdx] = useState(0);
  const [input, setInput] = useState('');
  const [answers, setAnswers] = useState<string[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState('');

  // Voice mode state
  const [isListening, setIsListening] = useState(false);
  const [isAiSpeaking, setIsAiSpeaking] = useState(false);
  const recognitionRef = useRef<any>(null);

  // Generating questions is a paid LLM call, so it must happen exactly once
  // per repo. React's StrictMode double-mounts effects in development, and
  // without this guard every dev page load billed us twice.
  const requestedRepo = useRef<string | null>(null);

  useEffect(() => {
    if (!repoId || requestedRepo.current === repoId) return;
    requestedRepo.current = repoId;

    // Staleness is decided by the ref, not by an effect-local flag.
    //
    // StrictMode mounts, cleans up, then mounts again on the same instance.
    // An effect-local flag is set false by that cleanup, while the remount is
    // turned away by the ref guard — so the one request in flight resolves
    // into a dead closure and the page sits on "Generating tailored
    // questions..." forever. The ref survives the remount, so testing against
    // it accepts that response while still dropping one for a repo the user
    // has since navigated away from.
    fetch(`${import.meta.env.VITE_API_URL}/api/interviews/questions?repo_full_name=${encodeURIComponent(repoId)}${targetParams ? `&${targetParams}` : ''}`, {
      credentials: 'include',
    })
    .then(async res => {
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        // The API explains exactly what is wrong — "analysis is still
        // running", "retry analyzing this repository". Showing our own
        // generic line instead threw that away.
        throw new Error(data?.error || 'Could not load questions for this repository.');
      }
      return data;
    })
    .then(data => {
      if (requestedRepo.current !== repoId) return; // a different repo is loading now
      const list = Array.isArray(data) ? data : data?.questions;
      if (Array.isArray(list) && list.length > 0) {
        setQuestions(list);
        setAnswers(new Array(list.length).fill(''));
      } else {
        setError('No questions came back for this repository.');
      }
    })
    .catch(err => {
      if (requestedRepo.current !== repoId) return;
      requestedRepo.current = null; // let a retry through
      setError(err.message);
    });
  }, [repoId]);

  // AI Speaking (Text-to-Speech) - PONYTAIL: Native Web Speech API, $0 cost
  useEffect(() => {
    if (questions.length === 0 || !isVoiceMode) return;

    const q = questions[currentQuestionIdx];
    const text = q.question_text || q.QuestionText || q.question;

    if (text && 'speechSynthesis' in window) {
      window.speechSynthesis.cancel();
      const utterance = new SpeechSynthesisUtterance(text);
      utterance.rate = 0.95;
      utterance.pitch = 1.0;

      utterance.onstart = () => setIsAiSpeaking(true);
      utterance.onend = () => setIsAiSpeaking(false);
      utterance.onerror = () => setIsAiSpeaking(false);

      // Small delay to feel natural
      setTimeout(() => window.speechSynthesis.speak(utterance), 500);
    }
  }, [currentQuestionIdx, questions, isVoiceMode]);

  // Cleanup speech on unmount
  useEffect(() => {
    return () => {
      if ('speechSynthesis' in window) window.speechSynthesis.cancel();
      if (recognitionRef.current) recognitionRef.current.stop();
    };
  }, []);

  const advance = (answer: string) => {
    window.speechSynthesis.cancel();
    if (recognitionRef.current) {
       recognitionRef.current.stop();
       setIsListening(false);
    }

    const nextAnswers = [...answers];
    nextAnswers[currentQuestionIdx] = answer;
    setAnswers(nextAnswers);
    setInput('');
    if (currentQuestionIdx < questions.length - 1) setCurrentQuestionIdx(i => i + 1);
    else submitInterview(nextAnswers);
  };

  const handleNext = () => input.trim() && advance(input);
  const handleSkip = () => advance("Skipped");

  // User Speaking (Speech-to-Text) - PONYTAIL: Native Web Speech API
  const toggleListening = () => {
    if (isListening) {
      if (recognitionRef.current) recognitionRef.current.stop();
      setIsListening(false);
      return;
    }

    const SpeechRecognition = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
    if (!SpeechRecognition) {
      alert("Voice recognition is not supported in this browser. Please use Chrome/Edge or type your answer.");
      return;
    }

    window.speechSynthesis.cancel(); // Stop AI if speaking
    setIsAiSpeaking(false);

    const recognition = new SpeechRecognition();
    recognition.continuous = true;
    recognition.interimResults = true;
    recognitionRef.current = recognition;

    recognition.onresult = (event: any) => {
      let currentTranscript = '';
      for (let i = 0; i < event.results.length; i++) {
        currentTranscript += event.results[i][0].transcript;
      }
      setInput(currentTranscript);
    };

    recognition.onerror = (event: any) => {
      console.error("Speech recognition error", event.error);
      setIsListening(false);
    };

    recognition.onend = () => {
      setIsListening(false);
    };

    recognition.start();
    setIsListening(true);
  };


  const submitInterview = async (finalAnswers: string[]) => {
    setIsSubmitting(true);
    try {
      const qaList = questions.map((q, idx) => ({
        question: q.question_text || q.QuestionText || q.question,
        answer: finalAnswers[idx]
      }));

      const res = await fetch(`${import.meta.env.VITE_API_URL}/api/interviews/submit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          repo_full_name: decodeURIComponent(repoId || ''),
          qa_list: qaList,
          mode: isVoiceMode ? 'voice' : 'text',
        })
      });

      const data = await res.json().catch(() => null);
      if (!res.ok) throw new Error(data?.error || 'Submission failed');
      navigate(`/report/${data.session_id}`);
    } catch (err: any) {
      setError(err.message);
      setIsSubmitting(false);
    }
  };

  if (error) {
    return <div className="min-h-screen flex items-center justify-center text-crit">Error: {error}</div>;
  }

  if (questions.length === 0 || isSubmitting) {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center bg-paper space-y-4">
        <Loader2 className="w-12 h-12 text-accent animate-spin" />
        <p className="text-ink-faint font-medium">{isSubmitting ? 'Evaluating answers...' : 'Generating tailored questions...'}</p>
      </div>
    );
  }

  const currentQuestion = questions[currentQuestionIdx];
  const questionText = currentQuestion.question_text || currentQuestion.QuestionText || currentQuestion.question;
  const snippet: string = currentQuestion.code_snippet || '';

  return (
    <div className="min-h-screen bg-paper text-ink flex flex-col">
      {/* Header */}
      <header className="p-4 border-b border-line flex items-center justify-between bg-surface/80 backdrop-blur-sm sticky top-0 z-10">
        <div className="font-mono text-sm font-semibold text-ink-faint tabular-nums">Question {currentQuestionIdx + 1} / {questions.length}</div>
        <div className="flex gap-2">
          <button
            onClick={() => window.speechSynthesis.cancel()}
            className="p-2 text-ink-faint hover:text-accent rounded-lg hover:bg-accent-soft transition-colors"
            title="Mute AI"
          >
            <VolumeX className="w-5 h-5" />
          </button>
          <button
            onClick={handleSkip}
            className="p-2 text-ink-faint hover:text-accent rounded-lg hover:bg-accent-soft transition-colors"
            title="Skip Question"
          >
            <SkipForward className="w-5 h-5" />
          </button>
        </div>
      </header>

      {/* Main Content */}
      <main className="flex-1 p-4 md:p-8 flex flex-col">
        <div className="flex-1 grid grid-cols-1 md:grid-cols-2 gap-6 max-w-6xl mx-auto w-full">
          
          {/* Left Panel: AI / Question */}
          <div className="flex flex-col items-center justify-center bg-surface border border-line rounded-2xl p-8 shadow-sm relative overflow-hidden">
            {/* Visualizer for Voice Mode */}
            {isVoiceMode && (
              <div className="mb-12 relative w-48 h-48 flex items-center justify-center">
                {isAiSpeaking && (
                  <>
                    <div className="absolute inset-0 border-4 border-accent/20 rounded-full animate-ping"></div>
                    <div className="absolute inset-4 border-4 border-accent/40 rounded-full animate-pulse"></div>
                  </>
                )}
                <div className={`w-32 h-32 rounded-full flex items-center justify-center shadow-xl transition-colors duration-500 z-10 ${isAiSpeaking ? 'bg-accent' : 'bg-line'}`}>
                  <Volume2 className={`w-12 h-12 ${isAiSpeaking ? 'text-white animate-pulse' : 'text-ink-faint'}`} />
                </div>
              </div>
            )}

            <div className="text-center space-y-4 z-10">
              <h2 className="font-display text-2xl md:text-3xl font-medium leading-relaxed">
                "{questionText}"
              </h2>
              {isVoiceMode && (
                <p className="text-accent text-sm font-mono font-medium tracking-wider uppercase animate-pulse">
                  {isAiSpeaking ? "AI is speaking..." : isListening ? "Listening to you..." : "Your turn to speak"}
                </p>
              )}
            </div>
            
            <div className="absolute top-4 left-4">
              <span className="px-3 py-1 bg-accent/10 text-accent rounded-full text-xs font-mono font-bold uppercase tracking-wider">
                NeuroFiq AI
              </span>
            </div>
          </div>

          {/* Right Panel: camera in a voice interview, the code under
              discussion in a text one. Someone who deliberately chose "Start
              Text Interview" should not be asked for their webcam. */}
          <div className="flex flex-col gap-4">
            {isVoiceMode && (
              <CameraPreview className="w-full flex-1 min-h-[220px] object-cover rounded-2xl shadow-sm border border-line" />
            )}
            {snippet ? (
              <div className="flex-1 min-h-0 bg-surface border border-line rounded-2xl overflow-hidden flex flex-col">
                <div className="flex items-center gap-2 px-4 py-3 border-b border-line">
                  <FileCode2 className="w-4 h-4 text-ink-faint" />
                  <span className="font-mono text-xs text-ink-soft truncate">
                    {currentQuestion.file_reference || 'Your code'}
                  </span>
                </div>
                <pre className="flex-1 overflow-auto p-4 text-xs font-mono text-ink-soft leading-relaxed whitespace-pre">
{snippet}
                </pre>
              </div>
            ) : !isVoiceMode ? (
              <div className="flex-1 min-h-[300px] bg-surface border border-line rounded-2xl flex items-center justify-center p-8 text-center">
                <p className="text-sm text-ink-faint">
                  This question is about your commit history rather than one file.
                </p>
              </div>
            ) : null}
          </div>

        </div>

        {/* Controls / Input Area */}
        <div className="w-full max-w-4xl mx-auto mt-6 bg-surface border border-line p-4 rounded-2xl shadow-sm">
          {isVoiceMode ? (
            <div className="flex flex-col md:flex-row items-center gap-4">
              <button
                onClick={toggleListening}
                className={`w-14 h-14 shrink-0 rounded-full flex items-center justify-center shadow-md transition-transform active:scale-95 ${
                  isListening ? 'bg-crit hover:bg-crit/90 animate-pulse' : 'bg-accent hover:bg-accent-dark'
                }`}
              >
                {isListening ? <MicOff className="w-6 h-6 text-white" /> : <Mic className="w-6 h-6 text-white" />}
              </button>

              <div className="w-full relative flex-1">
                 <textarea
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  placeholder="Your speech will appear here... (You can also type)"
                  className="w-full bg-paper border border-line rounded-xl p-3 text-ink focus:outline-none focus:border-accent resize-none min-h-[60px]"
                />
              </div>

              <button
                onClick={handleNext}
                className="px-6 h-14 bg-pass hover:bg-pass/90 text-white font-bold rounded-xl transition-colors disabled:opacity-50 disabled:cursor-not-allowed shadow-md shrink-0"
                disabled={input.trim() === ''}
              >
                Submit
              </button>
            </div>
          ) : (
            <div className="relative flex items-center gap-4">
              <textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder="Type your answer here..."
                className="w-full bg-paper border border-line rounded-xl p-4 text-ink focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent resize-none min-h-[80px]"
              />
              <button
                onClick={handleNext}
                className="px-6 h-[80px] bg-accent hover:bg-accent-dark text-white font-bold rounded-xl transition-colors disabled:opacity-50 shadow-md shrink-0 flex flex-col items-center justify-center gap-1"
                disabled={input.trim() === ''}
              >
                <Send className="w-5 h-5" />
                <span className="text-xs">Send</span>
              </button>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
