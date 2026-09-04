import sys
import os
import time
import grpc
from concurrent import futures
import logging
from dotenv import load_dotenv

# Add proto directory to sys path so Python can find interview_pb2
sys.path.append(os.path.join(os.path.dirname(__file__), "proto"))
import interview_pb2
import interview_pb2_grpc

from agno.agent import Agent
from agno.models.groq import Groq

load_dotenv()

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class InterviewService(interview_pb2_grpc.InterviewServiceServicer):
    def StreamInterview(self, request_iterator, context):
        logger.info("New Bi-directional stream started.")
        
        # Initialize the AI Agent for this specific session
        agent = Agent(
            model=Groq(id="llama-3.3-70b-versatile", api_key=os.getenv("GROQ_API_KEY")),
            description="You are a senior technical interviewer for a B2B enterprise software company. You are conducting a voice interview with a candidate.",
            instructions=[
                "Keep your responses very concise and conversational, maximum 2 sentences at a time.",
                "Act like a real human interviewer.",
                "Do not use markdown, formatting, or lists in your responses, as they will be spoken via Text-to-Speech.",
                "Acknowledge the candidate's answer and ask a logical follow-up technical question."
            ],
            markdown=False
        )
        
        for request in request_iterator:
            logger.info(f"Received candidate message: {request.text_transcript}")
            if not request.text_transcript.strip():
                continue
                
            # Intercept system initializations (e.g. Job Context)
            if request.text_transcript.startswith("[SYSTEM_INIT: JOB_CONTEXT:"):
                jd = request.text_transcript.replace("[SYSTEM_INIT: JOB_CONTEXT:", "").rstrip("]")
                logger.info(f"Setting Job Context for session {request.session_id}")
                agent.description = f"You are a senior technical interviewer for a B2B enterprise software company. You are hiring for this specific role. Job Description: {jd.strip()}. Evaluate the candidate strictly against these requirements."
                continue
                
            # Stream the response from the LLM
            response_stream = agent.run(request.text_transcript, stream=True)
            
            for chunk in response_stream:
                if chunk.content:
                    yield interview_pb2.AiResponse(
                        session_id=request.session_id,
                        text_chunk=chunk.content,
                        audio_chunk=b'',
                        is_complete=False
                    )
            
            # Send the final completion message for this turn
            yield interview_pb2.AiResponse(
                session_id=request.session_id,
                text_chunk="",
                audio_chunk=b'',
                is_complete=True
            )
        
        logger.info("Stream ended by client.")

def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    interview_pb2_grpc.add_InterviewServiceServicer_to_server(InterviewService(), server)
    
    port = "50051"
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"gRPC Server running on port {port}")
    
    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        logger.info("Shutting down gRPC server")
        server.stop(0)

if __name__ == "__main__":
    serve()
