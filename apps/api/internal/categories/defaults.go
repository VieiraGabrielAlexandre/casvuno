package categories

// defaultTree is the pt-BR starter set created with every household (F1.5). Families edit,
// archive and extend it freely; it is copied, never shared between households.
var defaultTree = []struct {
	kind     Kind
	name     string
	children []string
}{
	{KindExpense, "Moradia", []string{"Aluguel", "Condomínio", "Energia", "Água", "Gás", "Internet", "Manutenção"}},
	{KindExpense, "Alimentação", []string{"Mercado", "Restaurantes", "Delivery", "Padaria e café"}},
	{KindExpense, "Transporte", []string{"Combustível", "Transporte público", "Aplicativos", "Estacionamento", "Manutenção do veículo"}},
	{KindExpense, "Saúde", []string{"Plano de saúde", "Farmácia", "Consultas e exames"}},
	{KindExpense, "Educação", []string{"Mensalidades", "Cursos", "Material escolar"}},
	{KindExpense, "Lazer", []string{"Passeios", "Viagens", "Streaming"}},
	{KindExpense, "Compras", []string{"Vestuário", "Casa", "Eletrônicos"}},
	{KindExpense, "Contas e serviços", []string{"Telefone", "Assinaturas", "Tarifas bancárias", "Impostos e taxas"}},
	{KindExpense, "Cuidados pessoais", nil},
	{KindExpense, "Pets", nil},
	{KindExpense, "Presentes e doações", nil},
	{KindExpense, "Outras despesas", nil},
	{KindIncome, "Salário", nil},
	{KindIncome, "Renda extra", nil},
	{KindIncome, "Rendimentos", nil},
	{KindIncome, "Reembolsos", nil},
	{KindIncome, "Outras receitas", nil},
}
