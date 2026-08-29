import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { LandingPage } from './pages/LandingPage';
import { AuthChoice } from './pages/AuthChoice';
import { Dashboard } from './pages/Dashboard';
import { Repositories } from './pages/Repositories';
import { ReportsList } from './pages/ReportsList';
import { AnalysisProgress } from './pages/AnalysisProgress';
import { InterviewSession } from './pages/InterviewSession';
import { Report } from './pages/Report';
import { PublicReport } from './pages/PublicReport';
import { InviteLanding } from './pages/InviteLanding';
import { RecruiterDashboard } from './pages/RecruiterDashboard';
import { RecruiterReport } from './pages/RecruiterReport';
import { CompanyMap } from './pages/CompanyMap';
import { DashboardLayout } from './components/DashboardLayout';
import { AuthProvider } from './context/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';

function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<LandingPage />} />
          <Route path="/auth" element={<AuthChoice />} />

          {/* Link-only routes. Holding the URL is the authorisation, so these
              sit outside the login on purpose — a shared report has to open
              for a hiring manager who has never heard of us. */}
          <Route path="/r/:slug" element={<PublicReport />} />
          <Route path="/invite/:token" element={<InviteLanding />} />

          {/* Authenticated Routes with Sidebar */}
          <Route element={<ProtectedRoute />}>
            <Route element={<DashboardLayout />}>
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/repositories" element={<Repositories />} />
              <Route path="/reports" element={<ReportsList />} />
              <Route path="/analyze/:repoId" element={<AnalysisProgress />} />
              <Route path="/interview/:repoId" element={<InterviewSession />} />
              <Route path="/report/:sessionId" element={<Report />} />
              <Route path="/directory" element={<CompanyMap />} />
              <Route path="/hiring" element={<RecruiterDashboard />} />
              <Route path="/hiring/report/:sessionId" element={<RecruiterReport />} />
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}

export default App;
