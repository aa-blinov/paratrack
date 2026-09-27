package web

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// Inter, the UI face, embedded as TTF: the PDF core fonts are cp1252 and
// turned Cyrillic into dots. Licence: pdffonts/Inter-LICENSE.txt (OFL).
var (
	//go:embed pdffonts/Inter-Regular.ttf
	interRegular []byte
	//go:embed pdffonts/Inter-SemiBold.ttf
	interSemiBold []byte
)

// renderInvoicePDF builds a one-page A4 invoice in the reader's language.
// Layout is plain: title and number, seller and client, dates, the lines
// table, total, notes. It prints cleanly anywhere.
func renderInvoicePDF(inv invoiceVM, teamName string, lang i18n.Lang) ([]byte, error) {
	return renderDocPDF(inv, teamName, lang, false)
}

// renderDocPDF draws the invoice, or with act=true the certificate of
// completion for the same lines (statement + signature lines).
func renderDocPDF(inv invoiceVM, teamName string, lang i18n.Lang, act bool) ([]byte, error) {
	T := func(k string) string { return i18n.T(lang, k) }
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("Inter", "", interRegular)
	pdf.AddUTF8FontFromBytes("Inter", "B", interSemiBold)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetMargins(15, 15, 15)
	pdf.AddPage()
	grey := func() { pdf.SetTextColor(105, 105, 110) }
	ink := func() { pdf.SetTextColor(20, 20, 24) }
	ink()
	if img, typ := logoBytes(string(inv.Logo)); img != nil {
		opt := fpdf.ImageOptions{ImageType: typ, ReadDpi: false}
		pdf.RegisterImageOptionsReader("logo", opt, bytes.NewReader(img))
		if pdf.Ok() {
			pdf.ImageOptions("logo", 15, 15, 0, 12, false, opt, 0, "")
			pdf.SetY(15 + 12 + 4)
		} else {
			pdf.ClearError() // a broken logo mustn't cost the invoice
		}
	}

	// Title + number, date on the right.
	pdf.SetFont("Inter", "B", 18)
	title := T("inv.invoice") + " " + inv.Number
	if act {
		title = T("act.heading") + " " + inv.Number
	}
	pdf.CellFormat(130, 9, title, "", 0, "L", false, 0, "")
	pdf.SetFont("Inter", "", 9)
	grey()
	pdf.CellFormat(0, 9, T("pdf.issued")+" "+inv.IssuedLabel, "", 1, "R", false, 0, "")
	pdf.Ln(4)

	// Seller | client, then the period.
	y := pdf.GetY()
	party := func(x float64, label, name, details string) {
		pdf.SetXY(x, y)
		pdf.SetFont("Inter", "", 8)
		grey()
		pdf.CellFormat(85, 5, strings.ToUpper(label), "", 2, "L", false, 0, "")
		pdf.SetFont("Inter", "B", 11)
		ink()
		pdf.MultiCell(85, 6, name, "", "L", false)
		if details != "" {
			pdf.SetX(x)
			pdf.SetFont("Inter", "", 8.5)
			pdf.MultiCell(85, 4.2, details, "", "L", false)
		}
	}
	party(15, T("pdf.from"), teamName, inv.SellerDetails)
	yLeft := pdf.GetY()
	party(110, T("pdf.billedTo"), inv.ClientName, inv.ClientDetails)
	pdf.SetY(max(yLeft, pdf.GetY()) + 2)
	pdf.SetFont("Inter", "", 9)
	grey()
	pdf.CellFormat(0, 5, T("inv.period")+": "+inv.PeriodLabel, "", 1, "L", false, 0, "")
	ink()
	pdf.Ln(5)

	// Lines. Hairline rules, figures right-aligned.
	cols := []float64{95, 25, 30, 30}
	pdf.SetDrawColor(200, 200, 205)
	pdf.SetFont("Inter", "B", 9)
	for i, h := range []string{T("pdf.work"), T("inv.hours"), T("inv.rate"), T("inv.amount")} {
		align := "R"
		if i == 0 {
			align = "L"
		}
		pdf.CellFormat(cols[i], 8, h, "B", 0, align, false, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Inter", "", 9)
	for _, l := range inv.Lines {
		label := l.Label
		for pdf.GetStringWidth(label) > cols[0]-3 && len([]rune(label)) > 4 {
			label = truncateRunes(label, len([]rune(label))-2) + "…"
			label = strings.TrimSuffix(label, "……") // keep one ellipsis while shrinking
		}
		pdf.CellFormat(cols[0], 7, label, "B", 0, "L", false, 0, "")
		pdf.CellFormat(cols[1], 7, l.Hours, "B", 0, "R", false, 0, "")
		pdf.CellFormat(cols[2], 7, l.Rate, "B", 0, "R", false, 0, "")
		pdf.CellFormat(cols[3], 7, l.Amount, "B", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}
	pdf.SetFont("Inter", "B", 10)
	pdf.CellFormat(cols[0], 9, T("pdf.total"), "", 0, "L", false, 0, "")
	pdf.CellFormat(cols[1], 9, inv.Hours, "", 0, "R", false, 0, "")
	pdf.CellFormat(cols[2], 9, "", "", 0, "R", false, 0, "")
	pdf.CellFormat(cols[3], 9, inv.Total, "", 1, "R", false, 0, "")
	if inv.VATNote != "" {
		pdf.SetFont("Inter", "", 9)
		pdf.CellFormat(0, 6, inv.VATNote, "", 1, "L", false, 0, "")
	}
	if inv.Receipt != "" {
		pdf.SetFont("Inter", "", 9)
		pdf.MultiCell(0, 5, T("inv.receipt")+": "+inv.Receipt, "", "L", false)
	}
	if act {
		pdf.Ln(6)
		pdf.SetFont("Inter", "", 9)
		pdf.MultiCell(0, 5, T("act.statement"), "", "L", false)
		pdf.Ln(14)
		yy := pdf.GetY()
		for i, who := range []string{T("pdf.from"), T("pdf.billedTo")} {
			x := 15.0 + float64(i)*95
			pdf.Line(x, yy, x+80, yy)
			pdf.SetXY(x, yy+1)
			pdf.SetFont("Inter", "", 8)
			grey()
			pdf.CellFormat(80, 4, who, "", 0, "L", false, 0, "")
			ink()
		}
		pdf.Ln(8)
	}

	if inv.Notes != "" {
		pdf.Ln(6)
		pdf.SetFont("Inter", "", 9)
		pdf.MultiCell(0, 5, inv.Notes, "", "L", false)
	}
	if inv.PaymentURL != "" && !act {
		pdf.Ln(3)
		pdf.SetFont("Inter", "", 9)
		pdf.MultiCell(0, 5, T("inv.payOnline")+": "+inv.PaymentURL, "", "L", false)
	}
	pdf.Ln(6)
	pdf.SetFont("Inter", "", 8)
	grey()
	pdf.MultiCell(0, 4, T("inv.footNote"), "", "L", false)

	var buf strings.Builder
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// truncateRunes cuts a string to n runes (for PDF cells).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// invoicePDFName is the download filename.
func invoicePDFName(inv invoiceVM) string {
	return fmt.Sprintf("%s.pdf", strings.ReplaceAll(inv.Number, " ", "-"))
}

// logoBytes decodes the stored data: URL into image bytes and fpdf's type.
func logoBytes(v string) ([]byte, string) {
	for prefix, typ := range map[string]string{"data:image/png;base64,": "PNG", "data:image/jpeg;base64,": "JPG"} {
		if rest, ok := strings.CutPrefix(v, prefix); ok {
			b, err := base64.StdEncoding.DecodeString(rest)
			if err != nil {
				return nil, ""
			}
			return b, typ
		}
	}
	return nil, ""
}
