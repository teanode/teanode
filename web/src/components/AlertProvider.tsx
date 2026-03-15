import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Collapse from "@mui/material/Collapse";

type AlertSeverity = "success" | "error" | "warning" | "info";

interface AlertItem {
  id: number;
  message: string;
  severity: AlertSeverity;
  visible: boolean;
}

interface AlertContextValue {
  showAlert: (message: string, severity?: AlertSeverity) => void;
}

const AlertContext = createContext<AlertContextValue | null>(null);

export function useAlert(): AlertContextValue {
  const ctx = useContext(AlertContext);
  if (!ctx) throw new Error("useAlert must be used within AlertProvider");
  return ctx;
}

const MAX_VISIBLE = 2;
const AUTO_DISMISS_MS = 4000;
const ANIM_MS = 250;

/** Enter: fade in + rise. Exit: fade out + rise, then Collapse closes the gap. */
function AlertTransition({
  in: inProp,
  onExited,
  children,
}: {
  in: boolean;
  onExited: () => void;
  children: React.ReactNode;
}) {
  const [entered, setEntered] = useState(false);

  useEffect(() => {
    const id = requestAnimationFrame(() => setEntered(true));
    return () => cancelAnimationFrame(id);
  }, []);

  // If dismissed before the enter frame fired, skip animation entirely.
  useEffect(() => {
    if (!inProp && !entered) onExited();
  }, [inProp, entered, onExited]);

  const exiting = entered && !inProp;

  return (
    <Collapse in={!exiting} timeout={ANIM_MS} onExited={onExited}>
      {/* pb gives inter-alert spacing that collapses together with content */}
      <Box sx={{ pb: 1 }}>
        <Box
          sx={{
            transition: `transform ${ANIM_MS}ms ease, opacity ${ANIM_MS}ms ease`,
            transform: !entered || exiting ? "translateY(-8px)" : "none",
            opacity: !entered || exiting ? 0 : 1,
          }}
        >
          {children}
        </Box>
      </Box>
    </Collapse>
  );
}

export function AlertProvider({ children }: { children: React.ReactNode }) {
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  const nextId = useRef(0);
  const timers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

  const dismiss = useCallback((id: number) => {
    const timer = timers.current.get(id);
    if (timer) {
      clearTimeout(timer);
      timers.current.delete(id);
    }
    setAlerts((prev) =>
      prev.map((a) => (a.id === id ? { ...a, visible: false } : a)),
    );
  }, []);

  const handleExited = useCallback((id: number) => {
    setAlerts((prev) => prev.filter((a) => a.id !== id));
  }, []);

  const showAlert = useCallback(
    (message: string, severity: AlertSeverity = "success") => {
      const id = nextId.current++;

      setAlerts((prev) => {
        const next = [...prev, { id, message, severity, visible: true }];
        const visible = next.filter((a) => a.visible);
        const overflow = visible.length - MAX_VISIBLE;
        if (overflow > 0) {
          let evicted = 0;
          for (const a of next) {
            if (evicted >= overflow) break;
            if (a.visible && a.id !== id) {
              a.visible = false;
              const t = timers.current.get(a.id);
              if (t) {
                clearTimeout(t);
                timers.current.delete(a.id);
              }
              evicted++;
            }
          }
        }
        return next;
      });

      timers.current.set(
        id,
        setTimeout(() => dismiss(id), AUTO_DISMISS_MS),
      );
    },
    [dismiss],
  );

  return (
    <AlertContext.Provider value={{ showAlert }}>
      {children}
      <Box
        sx={{
          position: "fixed",
          top: 16,
          right: 16,
          zIndex: 9999,
          display: "flex",
          flexDirection: "column",
          maxWidth: 400,
          pointerEvents: "none",
        }}
      >
        {alerts.map((alert) => (
          <AlertTransition
            key={alert.id}
            in={alert.visible}
            onExited={() => handleExited(alert.id)}
          >
            <Alert
              severity={alert.severity}
              onClose={() => dismiss(alert.id)}
              sx={{
                pointerEvents: "auto",
                opacity: 0.92,
                boxShadow: 3,
                fontSize: "0.85rem",
              }}
            >
              {alert.message}
            </Alert>
          </AlertTransition>
        ))}
      </Box>
    </AlertContext.Provider>
  );
}
