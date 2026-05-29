import React from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Container from "@mui/material/Container";

interface SuggestionChipsProps {
  suggestions: string[];
  onSelect: (text: string) => void;
  disabled?: boolean;
}

/** Renders a row of clickable suggestion chips in the message timeline. */
export default function SuggestionChips({
  suggestions,
  onSelect,
  disabled,
}: SuggestionChipsProps) {
  if (suggestions.length === 0) return null;

  return (
    <Container maxWidth="md" sx={{ px: 2, pt: 1, pb: 1.5 }}>
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          columnGap: 0.75,
          rowGap: 1.25,
          justifyContent: "flex-end",
        }}
      >
        {suggestions.map((text, index) => (
          <Button
            key={index}
            variant="outlined"
            size="small"
            disabled={disabled}
            onClick={() => onSelect(text)}
            sx={{
              textTransform: "none",
              borderRadius: 4,
              fontSize: "0.8125rem",
              lineHeight: 1.6,
              minHeight: 34,
              px: 1.5,
              py: 0.5,
              minWidth: 0,
              borderColor: "divider",
              color: "text.primary",
              "&:hover": {
                borderColor: "primary.main",
                bgcolor: "action.hover",
              },
            }}
          >
            {text}
          </Button>
        ))}
      </Box>
    </Container>
  );
}
