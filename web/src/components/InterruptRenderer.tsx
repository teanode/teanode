import React from "react";
import { useTranslation } from "react-i18next";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import QuestionPanel from "./QuestionPanel";
import ApprovalPanel from "./ApprovalPanel";
import SurfaceRenderer from "./SurfaceRenderer";
import type {
  Interrupt,
  PendingQuestion,
  PendingApproval,
  Surface,
  SurfaceActionPayload,
} from "../types";

interface InterruptRendererProps {
  interrupts: Interrupt[];
  onAnswerQuestion: (
    answers: { questionId: string; answer: string; other?: string }[],
  ) => Promise<void> | void;
  onResolveApproval: (
    verdicts: { approvalId: string; verdict: string; reason?: string }[],
  ) => Promise<void> | void;
  onSurfaceAction: (
    action: SurfaceActionPayload,
    interruptId?: string,
  ) => Promise<void> | void;
  disabled?: boolean;
}

/**
 * Shared rendering path for all pending user-input interrupts. Questions and
 * approvals reuse their existing panels under the hood; choice/form/review
 * interrupts are rendered as schema-driven surfaces.
 */
export default function InterruptRenderer({
  interrupts,
  onAnswerQuestion,
  onResolveApproval,
  onSurfaceAction,
  disabled = false,
}: InterruptRendererProps) {
  if (interrupts.length === 0) return null;

  const questions: PendingQuestion[] = interrupts
    .filter((interrupt) => interrupt.kind === "question" && interrupt.question)
    .map((interrupt) => interrupt.question as PendingQuestion);

  const approvals: PendingApproval[] = interrupts
    .filter((interrupt) => interrupt.kind === "approval" && interrupt.approval)
    .map((interrupt) => interrupt.approval as PendingApproval);

  const richInterrupts = interrupts.filter(
    (interrupt) =>
      interrupt.kind === "choice" ||
      interrupt.kind === "form" ||
      interrupt.kind === "review",
  );

  return (
    <>
      {questions.length > 0 && (
        <QuestionPanel
          questions={questions}
          onSubmitAll={onAnswerQuestion}
          disabled={disabled}
        />
      )}
      {approvals.length > 0 && (
        <ApprovalPanel
          approvals={approvals}
          onResolve={onResolveApproval}
          disabled={disabled}
        />
      )}
      {richInterrupts.map((interrupt) => (
        <RichInterruptCard
          key={interrupt.interruptId}
          interrupt={interrupt}
          onSurfaceAction={onSurfaceAction}
          disabled={disabled}
        />
      ))}
    </>
  );
}

function RichInterruptCard({
  interrupt,
  onSurfaceAction,
  disabled,
}: {
  interrupt: Interrupt;
  onSurfaceAction: (
    action: SurfaceActionPayload,
    interruptId?: string,
  ) => Promise<void> | void;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const surface = interruptToSurface(interrupt, {
    approve: t("surface.approve"),
    reject: t("surface.reject"),
  });
  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
      <Box
        sx={{
          border: 1,
          borderColor: "primary.main",
          borderRadius: 1.5,
          p: 0.5,
        }}
      >
        {interrupt.prompt && (
          <Typography
            variant="body2"
            sx={{ px: 1.5, pt: 1, overflowWrap: "anywhere" }}
          >
            {interrupt.prompt}
          </Typography>
        )}
        <Box sx={{ p: 1 }}>
          <SurfaceRenderer
            surface={surface}
            disabled={disabled}
            onAction={(action) =>
              onSurfaceAction(action, interrupt.interruptId)
            }
          />
        </Box>
      </Box>
    </Container>
  );
}

/** Builds a renderable surface for a choice/form/review interrupt. */
function interruptToSurface(
  interrupt: Interrupt,
  labels: { approve: string; reject: string },
): Surface {
  const surfaceId = interrupt.surfaceId || interrupt.interruptId;
  const base: Surface = {
    surfaceId,
    schemaVersion: 1,
    location: "inline",
    title: interrupt.title,
    components: [],
  };

  if (interrupt.kind === "choice") {
    base.components = [
      {
        type: "ButtonRow",
        buttons: (interrupt.choices ?? []).map((choice) => ({
          label: choice,
          actionId: "select",
          value: choice,
        })),
      },
    ];
    return base;
  }

  if (interrupt.kind === "form") {
    base.components = [
      {
        type: "Form",
        fields: interrupt.fields ?? [],
        submitLabel: "Submit",
        submitActionId: "submit",
      },
    ];
    return base;
  }

  // review: render the supplied surface plus approve/reject actions.
  const reviewSurface = interrupt.surface;
  return {
    surfaceId,
    schemaVersion: reviewSurface?.schemaVersion ?? 1,
    location: "inline",
    title: interrupt.title ?? reviewSurface?.title,
    components: [
      ...(reviewSurface?.components ?? []),
      {
        type: "ButtonRow",
        buttons: [
          { label: labels.approve, actionId: "approve", style: "primary" },
          { label: labels.reject, actionId: "reject", style: "danger" },
        ],
      },
    ],
  };
}
