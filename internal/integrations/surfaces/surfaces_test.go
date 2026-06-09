package surfaces

import "testing"

func TestSurfaceValidate(t *testing.T) {
	valid := &Surface{
		SurfaceID:     "s1",
		SchemaVersion: SchemaVersion,
		Location:      SurfaceLocationInline,
		Components: []SurfaceComponent{
			{Type: ComponentTypeMarkdown, Text: "hello"},
			{Type: ComponentTypeSection, Title: "group", Children: []SurfaceComponent{
				{Type: ComponentTypeStatusBadge, Status: "success", Label: "ok"},
			}},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid surface, got error: %v", err)
	}

	badLocation := &Surface{Location: "elsewhere", Components: []SurfaceComponent{{Type: ComponentTypeMarkdown}}}
	if err := badLocation.Validate(); err == nil {
		t.Fatal("expected error for invalid location")
	}

	noComponents := &Surface{Location: SurfaceLocationInline}
	if err := noComponents.Validate(); err == nil {
		t.Fatal("expected error for surface without components")
	}

	unknownComponent := &Surface{Location: SurfaceLocationInline, Components: []SurfaceComponent{{Type: "Widget"}}}
	if err := unknownComponent.Validate(); err == nil {
		t.Fatal("expected error for unknown component type")
	}

	badField := &Surface{Location: SurfaceLocationInline, Components: []SurfaceComponent{
		{Type: ComponentTypeForm, Fields: []FormField{{Type: "Range", Name: "x"}}},
	}}
	if err := badField.Validate(); err == nil {
		t.Fatal("expected error for unknown form field type")
	}
}

func TestInterruptValidate(t *testing.T) {
	cases := []struct {
		name      string
		interrupt *Interrupt
		wantError bool
	}{
		{"choice ok", &Interrupt{Kind: InterruptKindChoice, Choices: []string{"a", "b"}}, false},
		{"choice missing choices", &Interrupt{Kind: InterruptKindChoice}, true},
		{"form ok", &Interrupt{Kind: InterruptKindForm, Fields: []FormField{{Type: FieldTypeTextInput, Name: "n"}}}, false},
		{"form missing fields", &Interrupt{Kind: InterruptKindForm}, true},
		{"bad kind", &Interrupt{Kind: "panic"}, true},
		{"question ok", &Interrupt{Kind: InterruptKindQuestion}, false},
	}
	for _, testCase := range cases {
		err := testCase.interrupt.Validate()
		if testCase.wantError && err == nil {
			t.Errorf("%s: expected error, got nil", testCase.name)
		}
		if !testCase.wantError && err != nil {
			t.Errorf("%s: expected no error, got %v", testCase.name, err)
		}
	}
}

func TestSurfaceBroker(t *testing.T) {
	broker := NewSurfaceBroker()
	surface := &Surface{SurfaceID: "s1", ConversationID: "c1"}
	broker.RegisterSurface(surface)
	broker.RegisterInterrupt(&Interrupt{InterruptID: "i1", ConversationID: "c1", SurfaceID: "s1"})

	if got := broker.LookupSurface("s1"); got == nil {
		t.Fatal("expected to find surface s1")
	}
	if surfaceList := broker.SurfacesForConversation("c1"); len(surfaceList) != 1 {
		t.Fatalf("expected 1 surface for c1, got %d", len(surfaceList))
	}
	if interruptList := broker.InterruptsForConversation("c1"); len(interruptList) != 1 {
		t.Fatalf("expected 1 interrupt for c1, got %d", len(interruptList))
	}
	if interruptList := broker.InterruptsForSurface("s1"); len(interruptList) != 1 {
		t.Fatalf("expected 1 interrupt for surface s1, got %d", len(interruptList))
	}
	if got := broker.LookupInterrupt("i1"); got == nil {
		t.Fatal("expected to find interrupt i1")
	}
	if got := broker.LookupInterrupt("missing"); got != nil {
		t.Fatal("expected nil for unknown interrupt")
	}

	// For*Conversation must return copies, not the live stored pointers, so a
	// caller marshaling the result cannot race with a concurrent re-register.
	listed := broker.SurfacesForConversation("c1")
	if len(listed) != 1 || listed[0] == surface {
		t.Fatal("expected SurfacesForConversation to return a copy, not the stored pointer")
	}

	broker.RemoveInterrupt("i1")
	if interruptList := broker.InterruptsForConversation("c1"); len(interruptList) != 0 {
		t.Fatalf("expected 0 interrupts after removal, got %d", len(interruptList))
	}
	broker.RemoveSurface("s1")
	if got := broker.LookupSurface("s1"); got != nil {
		t.Fatal("expected surface s1 to be removed")
	}
}
