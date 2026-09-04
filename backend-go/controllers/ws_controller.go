package controllers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	pb "github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/proto"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for dev
	},
}

// Deepgram STT response struct
type DeepgramResponse struct {
	IsFinal bool `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// HandleInterviewWebSocket establishes a WebSocket connection and proxies audio to Deepgram,
// while communicating with the Python AI worker via gRPC.
func HandleInterviewWebSocket(c *gin.Context) {
	clientConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}
	defer clientConn.Close()

	token := c.Query("token")
	sessionID := c.Query("repoName")
	if sessionID == "" {
		sessionID = "unknown_session"
	}
	
	// Fetch Job Description if token exists
	jobContext := ""
	if token != "" {
		var invite models.InterviewInvite
		if err := config.DB.Where("token = ?", token).First(&invite).Error; err == nil && invite.JobID != nil {
			var job models.Job
			if err := config.DB.Where("id = ?", invite.JobID).First(&job).Error; err == nil {
				jobContext = job.Title + "\n\n" + job.Description
			}
		}
	}

	log.Println("React Client connected to WebSocket Gateway for session:", sessionID)

	// Connect to Python gRPC AI Worker
	grpcConn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("Failed to connect to Python gRPC server:", err)
		return
	}
	defer grpcConn.Close()
	grpcClient := pb.NewInterviewServiceClient(grpcConn)
	stream, err := grpcClient.StreamInterview(context.Background())
	if err != nil {
		log.Println("Failed to start gRPC stream:", err)
		return
	}

	// Send initial context message if JD exists
	if jobContext != "" {
		stream.Send(&pb.CandidateMessage{
			SessionId:      sessionID,
			TextTranscript: "[SYSTEM_INIT: JOB_CONTEXT: " + jobContext + "]",
		})
	}

	// Connect to Deepgram (Nova-2 STT)
	deepgramURL := "wss://api.deepgram.com/v1/listen?model=nova-2&interim_results=true&punctuate=true&endpointing=300"
	apiKey := os.Getenv("DEEPGRAM_API_KEY")
	if apiKey == "" {
		log.Println("ERROR: DEEPGRAM_API_KEY is not set")
		return
	}

	header := http.Header{}
	header.Add("Authorization", "Token "+apiKey)

	dgConn, _, err := websocket.DefaultDialer.Dial(deepgramURL, header)
	if err != nil {
		log.Println("Failed to connect to Deepgram:", err)
		return
	}
	defer dgConn.Close()
	log.Println("Connected to Deepgram WebSocket API")

	done := make(chan struct{})

	// Goroutine 1: React -> Deepgram (Audio Chunks)
	go func() {
		defer close(done)
		for {
			messageType, message, err := clientConn.ReadMessage()
			if err != nil {
				log.Printf("React Read Error (or disconnected): %v\n", err)
				break
			}
			
			if messageType == websocket.BinaryMessage {
				if err := dgConn.WriteMessage(websocket.BinaryMessage, message); err != nil {
					log.Println("Deepgram Write Error:", err)
					break
				}
			}
		}
	}()

	// Goroutine 2: Deepgram -> Go -> Python gRPC -> React
	go func() {
		for {
			messageType, message, err := dgConn.ReadMessage()
			if err != nil {
				log.Println("Deepgram Read Error:", err)
				break
			}
			
			if messageType == websocket.TextMessage {
				// Parse Deepgram JSON to find is_final transcripts
				var dgResp DeepgramResponse
				if err := json.Unmarshal(message, &dgResp); err == nil {
					if dgResp.IsFinal && len(dgResp.Channel.Alternatives) > 0 {
						transcript := dgResp.Channel.Alternatives[0].Transcript
						if transcript != "" {
							log.Println("Final Transcript:", transcript)
							
							// Send transcript to Python via gRPC
							err = stream.Send(&pb.CandidateMessage{
								SessionId:      sessionID,
								TextTranscript: transcript,
								IsFinalAnswer:  false, // For now, treat everything as streaming input
							})
							if err != nil {
								log.Println("gRPC Send Error:", err)
							}
						}
					}
				}

				// Forward Deepgram STT response back to the browser for UI rendering
				if err := clientConn.WriteMessage(websocket.TextMessage, message); err != nil {
					log.Println("React Write Error:", err)
					break
				}
			}
		}
	}()

	// Goroutine 3: Python gRPC -> Go -> React (AI Responses)
	go func() {
		for {
			aiResp, err := stream.Recv()
			if err != nil {
				log.Println("gRPC Recv Error (or stream closed):", err)
				break
			}
			
			log.Println("Received AI Response from Python:", aiResp.TextChunk)
			
			// We can forward this AI text to the React UI as a custom JSON message
			// (Assuming React UI can handle custom JSON alongside Deepgram STT JSON)
			aiJson, _ := json.Marshal(map[string]interface{}{
				"type": "ai_response",
				"text": aiResp.TextChunk,
				"is_complete": aiResp.IsComplete,
			})
			if err := clientConn.WriteMessage(websocket.TextMessage, aiJson); err != nil {
				log.Println("React Write Error (AI Response):", err)
				break
			}
		}
	}()
	
	<-done
	stream.CloseSend()
	log.Println("Interview WebSocket session ended")
}
