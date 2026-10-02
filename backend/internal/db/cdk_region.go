package db

import "strings"

// Old rows remain NULL until their current page is refreshed from CardPlatform.
// Treating NULL as PH would mislabel old codes issued for another region.
func migrateCardplatformCDKRegionCol() error {
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name='payment_country'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE cardplatform_cdk_codes ADD COLUMN payment_country TEXT`)
	return err
}

func UpdateCardplatformCDKRegion(upstreamID int64, country string) error {
	return UpdateCardplatformCDKMetadata(upstreamID, "", country)
}

// The existing page refresh updates status and region in one SQLite write.
func UpdateCardplatformCDKMetadata(upstreamID int64, status, country string) error {
	if DB == nil || upstreamID <= 0 {
		return nil
	}
	status = strings.ToLower(strings.TrimSpace(status))
	_, err := DB.Exec(`UPDATE cardplatform_cdk_codes SET payment_country = ?, status = CASE WHEN ? != '' THEN ? ELSE status END WHERE upstream_id = ?`, strings.ToUpper(strings.TrimSpace(country)), status, status, upstreamID)
	return err
}
