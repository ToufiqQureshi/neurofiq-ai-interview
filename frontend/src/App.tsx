import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { LandingPage } from './pages/LandingPage';
import { AuthChoice } from './pages/AuthChoice';
import { Dashboard } from './pages/Dashboard';
import { Repositories } from './pages/Repositories';
import { ReportsList } from './pages/ReportsList';
import { AnalysisProgress } from './pages/AnalysisProgress';
import { InterviewSession } from './pages/InterviewSession';
import { Report } from './pages/Report';
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
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}

export default App;
